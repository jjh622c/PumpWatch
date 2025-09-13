package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"PumpWatch/internal/config"
	"PumpWatch/internal/models"
	"PumpWatch/internal/storage"
)

// DataCollectionManager interface for task managers that support data collection
type DataCollectionManager interface {
	StartDataCollection(symbol string, triggerTime time.Time) error
}

// UpbitMonitor monitors Upbit announcements for new listing detections
type UpbitMonitor struct {
	config         config.UpbitConfig
	httpClient     *http.Client
	taskManager    DataCollectionManager
	storageManager *storage.Manager
	
	// State management
	ctx            context.Context
	cancel         context.CancelFunc
	ticker         *time.Ticker
	running        bool
	mu             sync.RWMutex
	
	// Statistics
	stats          MonitorStats
	lastCheck      time.Time
	
	// Notification cache to prevent duplicate processing
	processedNotices map[string]time.Time
	processMutex     sync.RWMutex
}

// MonitorStats holds monitoring statistics
type MonitorStats struct {
	TotalPolls          int64     `json:"total_polls"`
	DetectedListings    int64     `json:"detected_listings"`
	SuccessfulTriggers  int64     `json:"successful_triggers"`
	FailedTriggers      int64     `json:"failed_triggers"`
	LastCheck           time.Time `json:"last_check"`
	LastDetection       time.Time `json:"last_detection"`
	AverageResponseTime time.Duration `json:"average_response_time"`
}


// NewUpbitMonitor creates a new Upbit monitor
func NewUpbitMonitor(ctx context.Context, config config.UpbitConfig, taskManager DataCollectionManager, storageManager *storage.Manager) (*UpbitMonitor, error) {
	monitorCtx, cancel := context.WithCancel(ctx)
	
	monitor := &UpbitMonitor{
		config:           config,
		taskManager:      taskManager,
		storageManager:   storageManager,
		ctx:              monitorCtx,
		cancel:           cancel,
		processedNotices: make(map[string]time.Time),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		stats: MonitorStats{
			LastCheck: time.Now(),
		},
	}
	
	return monitor, nil
}

// Start begins monitoring Upbit announcements
func (um *UpbitMonitor) Start() error {
	um.mu.Lock()
	defer um.mu.Unlock()
	
	if um.running {
		return fmt.Errorf("monitor is already running")
	}
	
	if !um.config.Enabled {
		return fmt.Errorf("Upbit monitoring is disabled in configuration")
	}
	
	um.ticker = time.NewTicker(um.config.PollInterval)
	um.running = true
	
	// Start monitoring goroutine
	go um.monitorLoop()
	
	fmt.Printf("📡 Upbit Monitor started - polling every %v\n", um.config.PollInterval)
	return nil
}

// Stop stops the Upbit monitor
func (um *UpbitMonitor) Stop(ctx context.Context) error {
	um.mu.Lock()
	defer um.mu.Unlock()
	
	if !um.running {
		return nil
	}
	
	um.running = false
	if um.ticker != nil {
		um.ticker.Stop()
	}
	
	um.cancel()
	
	// Clean up processed notices (older than 1 hour)
	um.cleanupProcessedNotices()
	
	fmt.Println("📡 Upbit Monitor stopped")
	return nil
}

// GetStats returns current monitoring statistics
func (um *UpbitMonitor) GetStats() MonitorStats {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return um.stats
}

// monitorLoop is the main monitoring loop
func (um *UpbitMonitor) monitorLoop() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ Upbit monitor panic recovered: %v\n", r)
		}
	}()
	
	for {
		select {
		case <-um.ctx.Done():
			return
		case <-um.ticker.C:
			um.checkForListings()
		}
	}
}

// checkForListings polls Upbit API for new announcements
func (um *UpbitMonitor) checkForListings() {
	startTime := time.Now()
	
	um.mu.Lock()
	um.stats.TotalPolls++
	um.stats.LastCheck = startTime
	um.mu.Unlock()
	
	// Fetch announcements
	announcements, err := um.fetchAnnouncements()
	if err != nil {
		fmt.Printf("⚠️ Failed to fetch Upbit announcements: %v\n", err)
		return
	}
	
	// Update response time
	responseTime := time.Since(startTime)
	um.updateAverageResponseTime(responseTime)
	
	// Process announcements for listings
	for _, announcement := range announcements {
		if um.shouldProcessAnnouncement(announcement) {
			if listingEvent := um.parseListingAnnouncement(announcement); listingEvent != nil {
				um.handleListingEvent(listingEvent)
			}
		}
	}
}

// fetchAnnouncements fetches latest announcements from Upbit API
func (um *UpbitMonitor) fetchAnnouncements() ([]UpbitAnnouncement, error) {
	req, err := http.NewRequestWithContext(um.ctx, "GET", um.config.APIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("User-Agent", um.config.UserAgent)
	
	resp, err := um.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	var response UpbitAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	
	if !response.Success {
		return nil, fmt.Errorf("API returned unsuccessful response")
	}
	
	return response.Data.Notices, nil
}

// shouldProcessAnnouncement determines if an announcement should be processed
func (um *UpbitMonitor) shouldProcessAnnouncement(announcement UpbitAnnouncement) bool {
	um.processMutex.RLock()
	_, alreadyProcessed := um.processedNotices[strconv.Itoa(announcement.ID)]
	um.processMutex.RUnlock()
	
	if alreadyProcessed {
		return false
	}
	
	// Check if announcement is recent (within 15 seconds, as per Flash-upbit logic)
	announcedAt, err := time.Parse("2006-01-02T15:04:05-07:00", announcement.ListedAt)
	if err != nil {
		return false
	}
	
	if time.Since(announcedAt) > 15*time.Second {
		return false
	}
	
	// Check badges for new listing (using API response flags)
	return announcement.NeedNewBadge && !announcement.NeedUpdateBadge
}

// parseListingAnnouncement parses announcement using Flash-upbit patterns
func (um *UpbitMonitor) parseListingAnnouncement(announcement UpbitAnnouncement) *models.ListingEvent {
	parser := NewListingParser()
	
	// Try to parse using Flash-upbit 5 patterns
	if result := parser.ParseListing(announcement.Title); result != nil {
		// Only process KRW listings
		if !result.IsKRWListing {
			return nil
		}
		
		announcedAt, _ := time.Parse("2006-01-02T15:04:05-07:00", announcement.ListedAt)
		
		listingEvent := &models.ListingEvent{
			ID:           strconv.Itoa(announcement.ID),
			Title:        announcement.Title,
			Symbol:       result.Symbol,
			Markets:      result.Markets,
			AnnouncedAt:  announcedAt,
			DetectedAt:   time.Now(),
			NoticeURL:    fmt.Sprintf("https://upbit.com/service_center/notice?id=%d", announcement.ID),
			TriggerTime:  announcedAt, // Use announcement time as trigger time
			IsKRWListing: result.IsKRWListing,
		}
		
		return listingEvent
	}
	
	return nil
}

// handleListingEvent processes a detected listing event
func (um *UpbitMonitor) handleListingEvent(event *models.ListingEvent) {
	// Mark as processed
	um.processMutex.Lock()
	um.processedNotices[event.ID] = time.Now()
	um.processMutex.Unlock()
	
	// Update statistics
	um.mu.Lock()
	um.stats.DetectedListings++
	um.stats.LastDetection = event.DetectedAt
	um.mu.Unlock()
	
	fmt.Printf("🚨 === New KRW Listing Detected! ===\n")
	fmt.Printf("💎 Symbol: %s\n", event.Symbol)
	fmt.Printf("📊 Markets: %v\n", event.Markets)
	fmt.Printf("📋 Title: %s\n", event.Title)
	fmt.Printf("🕐 Announced: %s\n", event.AnnouncedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("🎯 Detected: %s\n", event.DetectedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("⏱️ Detection delay: %v\n", event.DetectedAt.Sub(event.AnnouncedAt))
	fmt.Printf("🔗 URL: %s\n", event.NoticeURL)
	
	// Trigger data collection (-20 seconds from announcement time)
	if err := um.triggerDataCollection(event); err != nil {
		fmt.Printf("❌ Failed to trigger data collection: %v\n", err)
		um.mu.Lock()
		um.stats.FailedTriggers++
		um.mu.Unlock()
	} else {
		fmt.Printf("🚀 Data collection triggered successfully\n")
		um.mu.Lock()
		um.stats.SuccessfulTriggers++
		um.mu.Unlock()
	}
}

// triggerDataCollection initiates data collection for the listing event
func (um *UpbitMonitor) triggerDataCollection(event *models.ListingEvent) error {
	// Create collection event starting 20 seconds before announcement
	collectionStartTime := event.AnnouncedAt.Add(-20 * time.Second)
	
	fmt.Printf("📡 Starting data collection for %s\n", event.Symbol)
	fmt.Printf("⏰ Collection window: %s to %s (40 seconds)\n",
		collectionStartTime.Format("15:04:05"),
		event.AnnouncedAt.Add(20*time.Second).Format("15:04:05"))
	
	// Trigger WebSocket Task Manager to start collection
	if err := um.taskManager.StartDataCollection(event.Symbol, event.AnnouncedAt); err != nil {
		return fmt.Errorf("failed to start WebSocket data collection: %w", err)
	}
	
	// Store event metadata
	if err := um.storageManager.StoreListingEvent(event); err != nil {
		fmt.Printf("⚠️ Failed to store listing event metadata: %v\n", err)
	}
	
	return nil
}

// Helper methods

func (um *UpbitMonitor) updateAverageResponseTime(responseTime time.Duration) {
	um.mu.Lock()
	defer um.mu.Unlock()
	
	// Simple moving average
	if um.stats.AverageResponseTime == 0 {
		um.stats.AverageResponseTime = responseTime
	} else {
		um.stats.AverageResponseTime = (um.stats.AverageResponseTime + responseTime) / 2
	}
}

func (um *UpbitMonitor) cleanupProcessedNotices() {
	um.processMutex.Lock()
	defer um.processMutex.Unlock()
	
	cutoff := time.Now().Add(-1 * time.Hour)
	for id, processedAt := range um.processedNotices {
		if processedAt.Before(cutoff) {
			delete(um.processedNotices, id)
		}
	}
}

// UpbitAPIResponse represents the full Upbit API response
type UpbitAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		TotalPages   int                  `json:"total_pages"`
		TotalCount   int                  `json:"total_count"`
		Notices      []UpbitAnnouncement  `json:"notices"`
		FixedNotices []UpbitAnnouncement  `json:"fixed_notices"`
	} `json:"data"`
}

// UpbitAnnouncement represents an Upbit API announcement
type UpbitAnnouncement struct {
	ID              int    `json:"id"`
	Title           string `json:"title"`
	ListedAt        string `json:"listed_at"`
	FirstListedAt   string `json:"first_listed_at"`
	Category        string `json:"category"`
	NeedNewBadge    bool   `json:"need_new_badge"`
	NeedUpdateBadge bool   `json:"need_update_badge"`
}