package models

import "time"

// ListingEvent represents a detected listing announcement
type ListingEvent struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Symbol       string    `json:"symbol"`
	Markets      []string  `json:"markets"`
	AnnouncedAt  time.Time `json:"announced_at"`
	DetectedAt   time.Time `json:"detected_at"`
	NoticeURL    string    `json:"notice_url"`
	TriggerTime  time.Time `json:"trigger_time"`
	IsKRWListing bool      `json:"is_krw_listing"`
}
