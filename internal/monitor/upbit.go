package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	ctx     context.Context
	cancel  context.CancelFunc
	ticker  *time.Ticker
	running bool
	mu      sync.RWMutex

	// Statistics
	stats     MonitorStats
	lastCheck time.Time

	// 🆕 Persistent state management (replaces processedNotices map)
	persistentState *PersistentState

	// 📊 Performance analysis for delay investigation
	performanceAnalyzer *PerformanceAnalyzer

	// 🔍 Comprehensive delay analysis
	delayAnalyzer *DelayAnalyzer
}

// MonitorStats holds monitoring statistics
type MonitorStats struct {
	TotalPolls          int64         `json:"total_polls"`
	DetectedListings    int64         `json:"detected_listings"`
	SuccessfulTriggers  int64         `json:"successful_triggers"`
	FailedTriggers      int64         `json:"failed_triggers"`
	LastCheck           time.Time     `json:"last_check"`
	LastDetection       time.Time     `json:"last_detection"`
	AverageResponseTime time.Duration `json:"average_response_time"`
}

// NewUpbitMonitor creates a new Upbit monitor
func NewUpbitMonitor(ctx context.Context, config config.UpbitConfig, taskManager DataCollectionManager, storageManager *storage.Manager) (*UpbitMonitor, error) {
	monitorCtx, cancel := context.WithCancel(ctx)

	// 🆕 Initialize persistent state manager
	persistentState, err := NewPersistentState("data/monitor")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize persistent state: %w", err)
	}

	// 📊 Initialize performance analyzer for delay investigation
	performanceAnalyzer := NewPerformanceAnalyzer()
	if err := performanceAnalyzer.StartAnalysis(monitorCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize performance analyzer: %w", err)
	}

	// 🔍 Initialize comprehensive delay analyzer
	delayAnalyzer := NewDelayAnalyzer(performanceAnalyzer, persistentState)

	monitor := &UpbitMonitor{
		config:              config,
		taskManager:         taskManager,
		storageManager:      storageManager,
		ctx:                 monitorCtx,
		cancel:              cancel,
		persistentState:     persistentState,     // 🆕 Use persistent state instead of map
		performanceAnalyzer: performanceAnalyzer, // 📊 Performance tracking
		delayAnalyzer:       delayAnalyzer,       // 🔍 Delay analysis
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

	// 🆕 Force cleanup and save persistent state
	if err := um.persistentState.ForceCleanup(); err != nil {
		fmt.Printf("⚠️ Failed to cleanup persistent state: %v\n", err)
	}

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
		um.persistentState.RecordError(fmt.Sprintf("API fetch failed: %v", err)) // 🆕 Record error
		return
	}

	// Update response time and persistent statistics
	responseTime := time.Since(startTime)
	um.updateAverageResponseTime(responseTime)
	um.persistentState.UpdatePollStatistics() // 🆕 Update persistent statistics

	// Process announcements for listings
	for _, announcement := range announcements {
		if um.shouldProcessAnnouncement(announcement) {
			if listingEvent := um.parseListingAnnouncement(announcement); listingEvent != nil {
				um.handleListingEvent(listingEvent)
			}
		}
	}
}

// fetchAnnouncements fetches latest announcements from Upbit API with performance tracking
func (um *UpbitMonitor) fetchAnnouncements() ([]UpbitAnnouncement, error) {
	// 📊 Start performance tracking
	startTime := time.Now()
	var statusCode int
	var success bool
	var errorMessage string
	var responseSize int64
	var announcementCount int
	var newListingDetected bool

	// Ensure performance is recorded regardless of outcome
	defer func() {
		endTime := time.Now()
		um.performanceAnalyzer.RecordAPICall(
			startTime, endTime, statusCode, success, errorMessage,
			responseSize, announcementCount, newListingDetected,
		)
	}()

	req, err := http.NewRequestWithContext(um.ctx, "GET", um.config.APIURL, nil)
	if err != nil {
		errorMessage = fmt.Sprintf("failed to create request: %v", err)
		return nil, fmt.Errorf(errorMessage)
	}

	req.Header.Set("User-Agent", um.config.UserAgent)

	resp, err := um.httpClient.Do(req)
	if err != nil {
		errorMessage = fmt.Sprintf("HTTP request failed: %v", err)
		return nil, fmt.Errorf(errorMessage)
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		errorMessage = fmt.Sprintf("API returned status: %d", resp.StatusCode)
		return nil, fmt.Errorf(errorMessage)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errorMessage = fmt.Sprintf("failed to read response: %v", err)
		return nil, fmt.Errorf(errorMessage)
	}

	responseSize = int64(len(body))

	var response UpbitAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		errorMessage = fmt.Sprintf("failed to parse JSON: %v", err)
		return nil, fmt.Errorf(errorMessage)
	}

	if !response.Success {
		errorMessage = "API returned unsuccessful response"
		return nil, fmt.Errorf(errorMessage)
	}

	// 📊 Record successful metrics
	success = true
	announcementCount = len(response.Data.Notices)

	// Check if there are any new KRW listings in this response
	for _, announcement := range response.Data.Notices {
		if um.wouldProcessAnnouncement(announcement) {
			newListingDetected = true
			break
		}
	}

	return response.Data.Notices, nil
}

// shouldProcessAnnouncement determines if an announcement should be processed
func (um *UpbitMonitor) shouldProcessAnnouncement(announcement UpbitAnnouncement) bool {
	// 🆕 Check persistent state instead of memory map
	noticeID := strconv.Itoa(announcement.ID)
	if um.persistentState.IsNoticeProcessed(noticeID) {
		return false
	}

	return um.wouldProcessAnnouncement(announcement)
}

// wouldProcessAnnouncement checks if an announcement would be processed (without state check)
func (um *UpbitMonitor) wouldProcessAnnouncement(announcement UpbitAnnouncement) bool {

	// Check if announcement is recent (within 30 minutes to catch all listings)
	// Fixed: Extended from 15 seconds to 30 minutes to prevent missing listings
	announcedAt, err := time.Parse("2006-01-02T15:04:05-07:00", announcement.ListedAt)
	if err != nil {
		return false
	}

	if time.Since(announcedAt) > 30*time.Minute {
		return false
	}

	// Fixed: Accept both new listings and updates for KRW-related announcements
	// Check if it's KRW-related content first
	hasKRWContent := strings.Contains(strings.ToLower(announcement.Title), "krw") ||
		strings.Contains(strings.ToLower(announcement.Title), "신규 거래지원") ||
		strings.Contains(strings.ToLower(announcement.Title), "마켓")

	// Accept new listings OR updates that contain KRW-related content
	return announcement.NeedNewBadge || (announcement.NeedUpdateBadge && hasKRWContent)
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
	// 🆕 Create detailed notice record for persistent storage
	noticeRecord := NoticeRecord{
		AnnouncedAt:   event.AnnouncedAt,
		DetectedAt:    event.DetectedAt,
		Symbol:        event.Symbol,
		Title:         event.Title,
		DelayDuration: event.DetectedAt.Sub(event.AnnouncedAt),
		Success:       true, // Will be updated if collection fails
	}

	// Mark as processed in persistent state
	um.persistentState.MarkNoticeProcessed(event.ID, noticeRecord)

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

		// 🆕 Update persistent state with failure
		noticeRecord.Success = false
		um.persistentState.MarkNoticeProcessed(event.ID, noticeRecord)
		um.persistentState.RecordError(fmt.Sprintf("Data collection failed for %s: %v", event.Symbol, err))
	} else {
		fmt.Printf("🚀 Data collection triggered successfully\n")
		um.mu.Lock()
		um.stats.SuccessfulTriggers++
		um.mu.Unlock()

		// 🆕 Confirm success in persistent state
		um.persistentState.MarkNoticeProcessed(event.ID, noticeRecord)
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

// 🆕 GetHealthStatus returns comprehensive system health status
func (um *UpbitMonitor) GetHealthStatus() map[string]interface{} {
	um.mu.RLock()
	defer um.mu.RUnlock()

	health := um.persistentState.GetHealthStatus()
	health["monitor_running"] = um.running
	health["last_check"] = um.lastCheck.Format("2006-01-02 15:04:05")
	health["config_enabled"] = um.config.Enabled
	health["poll_interval"] = um.config.PollInterval.String()

	// 📊 Add performance analysis data
	performanceReport := um.performanceAnalyzer.GetPerformanceReport()
	health["performance_score"] = performanceReport["performance_score"]
	health["api_metrics"] = performanceReport["api_metrics"]
	health["system_metrics"] = performanceReport["system_metrics"]
	health["recommendations"] = performanceReport["recommendations"]

	return health
}

// 🆕 PrintDetailedStatus prints comprehensive status information
func (um *UpbitMonitor) PrintDetailedStatus() {
	health := um.GetHealthStatus()
	stats := um.persistentState.GetStatistics()

	fmt.Printf("\n📊 === Upbit Monitor Status ===\n")
	fmt.Printf("🔄 Running: %v | Health Score: %v/100 | Performance Score: %v/100\n",
		health["monitor_running"], health["health_score"], health["performance_score"])
	fmt.Printf("📡 Total Polls: %v | Detected Listings: %v\n", health["total_polls"], health["detected_listings"])
	fmt.Printf("⏱️ Average Delay: %v | Max Delay: %v\n", health["average_delay"], health["max_delay"])
	fmt.Printf("❌ Consecutive Failures: %v | Processed Notices: %v\n", health["consecutive_failures"], health["processed_notices"])
	fmt.Printf("💾 Uptime: %v | Last Poll: %v\n", health["uptime"], health["last_poll_age"])

	// 📊 Print performance analysis report
	um.performanceAnalyzer.PrintPerformanceReport()

	// 🔍 Print comprehensive delay analysis if performance issues detected
	if health["performance_score"].(int) < 80 || health["health_score"].(int) < 80 {
		fmt.Printf("\n🚨 === 성능 문제 감지 - 상세 지연 분석 실행 ===\n")
		um.delayAnalyzer.PrintDelayAnalysisReport()
	}

	// Show recent processed notices (last 1 hour)
	if len(stats.ProcessedNotices) > 0 {
		fmt.Printf("\n📋 Recent Listings (Last Hour):\n")
		for id, record := range stats.ProcessedNotices {
			status := "✅"
			if !record.Success {
				status = "❌"
			}
			fmt.Printf("  %s %s (%s) - Delay: %v\n", status, record.Symbol, id, record.DelayDuration)
		}
	}
	fmt.Printf("===============================\n\n")
}

// UpbitAPIResponse represents the full Upbit API response
type UpbitAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		TotalPages   int                 `json:"total_pages"`
		TotalCount   int                 `json:"total_count"`
		Notices      []UpbitAnnouncement `json:"notices"`
		FixedNotices []UpbitAnnouncement `json:"fixed_notices"`
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
