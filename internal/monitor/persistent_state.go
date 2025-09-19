package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// PersistentState manages persistent storage of monitor state
type PersistentState struct {
	filePath      string
	mutex         sync.RWMutex
	state         *MonitorState
	lastSaveTime  time.Time
	saveInterval  time.Duration
	maxRetention  time.Duration
}

// MonitorState represents the persistent state data
type MonitorState struct {
	// Processed notice tracking
	ProcessedNotices map[string]NoticeRecord `json:"processed_notices"`

	// Statistics and metadata
	LastPoll         time.Time `json:"last_poll"`
	TotalPolls       int64     `json:"total_polls"`
	DetectedListings int64     `json:"detected_listings"`
	SystemStartTime  time.Time `json:"system_start_time"`

	// Performance tracking
	LastDetectionDelay time.Duration `json:"last_detection_delay"`
	AverageDelay       time.Duration `json:"average_delay"`
	MaxDelay           time.Duration `json:"max_delay"`

	// System health indicators
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastErrorTime       time.Time `json:"last_error_time"`
	LastErrorMessage    string    `json:"last_error_message"`

	// Version for compatibility
	Version string `json:"version"`
}

// NoticeRecord tracks individual processed notices
type NoticeRecord struct {
	ProcessedAt   time.Time     `json:"processed_at"`
	AnnouncedAt   time.Time     `json:"announced_at"`
	DetectedAt    time.Time     `json:"detected_at"`
	Symbol        string        `json:"symbol"`
	Title         string        `json:"title"`
	DelayDuration time.Duration `json:"delay_duration"`
	Success       bool          `json:"success"`
}

// NewPersistentState creates a new persistent state manager
func NewPersistentState(dataDir string) (*PersistentState, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	filePath := filepath.Join(dataDir, "monitor_state.json")

	ps := &PersistentState{
		filePath:     filePath,
		saveInterval: 30 * time.Second,  // Save every 30 seconds
		maxRetention: 24 * time.Hour,    // Keep records for 24 hours
		state: &MonitorState{
			ProcessedNotices:    make(map[string]NoticeRecord),
			SystemStartTime:     time.Now(),
			ConsecutiveFailures: 0,
			Version:            "2.0",
		},
	}

	// Load existing state
	if err := ps.loadState(); err != nil {
		fmt.Printf("⚠️ Failed to load existing state, starting fresh: %v\n", err)
	}

	fmt.Printf("💾 Persistent state initialized: %s\n", filePath)
	fmt.Printf("📊 Loaded %d processed notices\n", len(ps.state.ProcessedNotices))

	return ps, nil
}

// IsNoticeProcessed checks if a notice has already been processed
func (ps *PersistentState) IsNoticeProcessed(noticeID string) bool {
	ps.mutex.RLock()
	defer ps.mutex.RUnlock()

	record, exists := ps.state.ProcessedNotices[noticeID]
	if !exists {
		return false
	}

	// Consider notices older than maxRetention as not processed (auto-cleanup)
	if time.Since(record.ProcessedAt) > ps.maxRetention {
		return false
	}

	return true
}

// MarkNoticeProcessed records a notice as processed
func (ps *PersistentState) MarkNoticeProcessed(noticeID string, record NoticeRecord) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	record.ProcessedAt = time.Now()
	ps.state.ProcessedNotices[noticeID] = record

	// Update statistics
	ps.state.DetectedListings++

	// Update delay statistics if this was a successful detection
	if record.Success && record.DelayDuration > 0 {
		ps.updateDelayStatistics(record.DelayDuration)
	}

	// Auto-save if enough time has passed
	if time.Since(ps.lastSaveTime) > ps.saveInterval {
		go ps.SaveState() // Non-blocking save
	}
}

// UpdatePollStatistics updates polling-related statistics
func (ps *PersistentState) UpdatePollStatistics() {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	ps.state.TotalPolls++
	ps.state.LastPoll = time.Now()
	ps.state.ConsecutiveFailures = 0 // Reset on successful poll
}

// RecordError records an error event
func (ps *PersistentState) RecordError(errorMsg string) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	ps.state.ConsecutiveFailures++
	ps.state.LastErrorTime = time.Now()
	ps.state.LastErrorMessage = errorMsg

	// Force save on errors for debugging
	go ps.SaveState()
}

// GetStatistics returns current statistics
func (ps *PersistentState) GetStatistics() MonitorState {
	ps.mutex.RLock()
	defer ps.mutex.RUnlock()

	// Create a copy to avoid concurrent access issues
	stateCopy := *ps.state
	stateCopy.ProcessedNotices = make(map[string]NoticeRecord)

	// Copy only recent notices for statistics
	cutoff := time.Now().Add(-1 * time.Hour)
	for id, record := range ps.state.ProcessedNotices {
		if record.ProcessedAt.After(cutoff) {
			stateCopy.ProcessedNotices[id] = record
		}
	}

	return stateCopy
}

// CleanupOldRecords removes records older than maxRetention
func (ps *PersistentState) CleanupOldRecords() {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	cutoff := time.Now().Add(-ps.maxRetention)
	cleaned := 0

	for id, record := range ps.state.ProcessedNotices {
		if record.ProcessedAt.Before(cutoff) {
			delete(ps.state.ProcessedNotices, id)
			cleaned++
		}
	}

	if cleaned > 0 {
		fmt.Printf("🧹 Cleaned up %d old notice records\n", cleaned)
		go ps.SaveState() // Save after cleanup
	}
}

// SaveState saves the current state to disk
func (ps *PersistentState) SaveState() error {
	ps.mutex.RLock()
	defer ps.mutex.RUnlock()

	// Create temporary file for atomic write
	tempFile := ps.filePath + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer file.Close()

	// Encode state to JSON with proper formatting
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(ps.state); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to encode state: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, ps.filePath); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	ps.lastSaveTime = time.Now()
	return nil
}

// loadState loads state from disk
func (ps *PersistentState) loadState() error {
	file, err := os.Open(ps.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No existing state file, use defaults
		}
		return fmt.Errorf("failed to open state file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var loadedState MonitorState
	if err := json.Unmarshal(data, &loadedState); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	// Validate loaded state
	if loadedState.ProcessedNotices == nil {
		loadedState.ProcessedNotices = make(map[string]NoticeRecord)
	}

	// Version compatibility check
	if loadedState.Version != ps.state.Version {
		fmt.Printf("⚠️ State file version mismatch (file: %s, current: %s), starting fresh\n",
			loadedState.Version, ps.state.Version)
		return nil
	}

	ps.state = &loadedState
	fmt.Printf("✅ Loaded state: %d notices, %d total polls, %d detected listings\n",
		len(ps.state.ProcessedNotices), ps.state.TotalPolls, ps.state.DetectedListings)

	return nil
}

// updateDelayStatistics updates detection delay statistics
func (ps *PersistentState) updateDelayStatistics(delay time.Duration) {
	ps.state.LastDetectionDelay = delay

	// Update max delay
	if delay > ps.state.MaxDelay {
		ps.state.MaxDelay = delay
	}

	// Update average delay (simple moving average)
	if ps.state.AverageDelay == 0 {
		ps.state.AverageDelay = delay
	} else {
		ps.state.AverageDelay = (ps.state.AverageDelay + delay) / 2
	}
}

// GetHealthStatus returns system health indicators
func (ps *PersistentState) GetHealthStatus() map[string]interface{} {
	ps.mutex.RLock()
	defer ps.mutex.RUnlock()

	uptime := time.Since(ps.state.SystemStartTime)
	lastPollAge := time.Since(ps.state.LastPoll)

	health := map[string]interface{}{
		"uptime":               uptime.String(),
		"last_poll_age":        lastPollAge.String(),
		"consecutive_failures": ps.state.ConsecutiveFailures,
		"total_polls":          ps.state.TotalPolls,
		"detected_listings":    ps.state.DetectedListings,
		"processed_notices":    len(ps.state.ProcessedNotices),
		"last_detection_delay": ps.state.LastDetectionDelay.String(),
		"average_delay":        ps.state.AverageDelay.String(),
		"max_delay":            ps.state.MaxDelay.String(),
	}

	// Health scoring
	var healthScore int = 100

	if ps.state.ConsecutiveFailures > 3 {
		healthScore -= 30
	}
	if lastPollAge > 30*time.Second {
		healthScore -= 20
	}
	if ps.state.AverageDelay > 5*time.Minute {
		healthScore -= 25
	}

	health["health_score"] = healthScore

	return health
}

// ForceCleanup performs immediate cleanup and save
func (ps *PersistentState) ForceCleanup() error {
	ps.CleanupOldRecords()
	return ps.SaveState()
}

// ConvertIDToString safely converts various ID types to string
func ConvertIDToString(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}