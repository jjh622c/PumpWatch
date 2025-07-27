package callback

import (
	"context"
	"log"
	"sync"
	"time"

	"noticepumpcatch/internal/signals"
)

// CallbackManager 외부 콜백 관리자
type CallbackManager struct {
	listingCallbacks []signals.ListingCallback
	mu               sync.RWMutex
	ctx              context.Context    // 🔥 context 추가
	cancel           context.CancelFunc // 🔥 취소 함수 추가
}

// NewCallbackManager 콜백 관리자 생성
func NewCallbackManager() *CallbackManager {
	ctx, cancel := context.WithCancel(context.Background()) // 🔥 컨텍스트 생성

	return &CallbackManager{
		listingCallbacks: make([]signals.ListingCallback, 0),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Stop 콜백 관리자 중지
func (cm *CallbackManager) Stop() {
	if cm.cancel != nil {
		cm.cancel()
	}
	log.Printf("✅ 콜백 관리자 중지 완료")
}

// RegisterListingCallback 상장공시 콜백 등록
func (cm *CallbackManager) RegisterListingCallback(callback signals.ListingCallback) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.listingCallbacks = append(cm.listingCallbacks, callback)
	log.Printf("📝 상장공시 콜백 등록: 총 %d개", len(cm.listingCallbacks))
}

// TriggerListingAnnouncement 상장공시 신호 트리거 (외부에서 호출)
func (cm *CallbackManager) TriggerListingAnnouncement(symbol, exchange, source string, confidence float64) {
	signal := signals.ListingSignal{
		Symbol:     symbol,
		Exchange:   exchange,
		Timestamp:  time.Now(),
		Confidence: confidence,
		Source:     source,
		Metadata:   make(map[string]interface{}),
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 등록된 모든 콜백에 신호 전달
	for _, callback := range cm.listingCallbacks {
		go func(cb signals.ListingCallback) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("❌ 콜백 실행 중 오류: %v", r)
				}
			}()

			// 🔥 컨텍스트 체크 추가
			select {
			case <-cm.ctx.Done():
				return // 시스템 종료 중이면 콜백 실행 중단
			default:
			}

			cb.OnListingAnnouncement(signal)
		}(callback)
	}

	log.Printf("📢 상장공시 신호 전달: %s (신뢰도: %.2f%%, 콜백: %d개)",
		symbol, confidence, len(cm.listingCallbacks))
}

// GetCallbackStats 콜백 통계 조회
func (cm *CallbackManager) GetCallbackStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"listing_callbacks": len(cm.listingCallbacks),
	}
}
