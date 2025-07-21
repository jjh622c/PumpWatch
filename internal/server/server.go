package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"noticepumpcatch/internal/memory"
	"noticepumpcatch/internal/monitor"
	"noticepumpcatch/internal/notification"
	"noticepumpcatch/internal/websocket"
)

// Server HTTP 서버
type Server struct {
	port            int
	memManager      *memory.Manager
	wsManager       *websocket.BinanceWebSocket
	notifManager    *notification.Manager
	perfMonitor     *monitor.PerformanceMonitor
	sysMonitor      *monitor.SystemMonitor
	mu              sync.RWMutex
	startTime       time.Time
	requestCount    int
	lastRequestTime time.Time
}

// NewServer HTTP 서버 생성
func NewServer(port int, mm *memory.Manager, ws *websocket.BinanceWebSocket, 
	nm *notification.Manager, pm *monitor.PerformanceMonitor, sm *monitor.SystemMonitor) *Server {
	return &Server{
		port:            port,
		memManager:      mm,
		wsManager:       ws,
		notifManager:    nm,
		perfMonitor:     pm,
		sysMonitor:      sm,
		startTime:       time.Now(),
		lastRequestTime: time.Now(),
	}
}

// Start 서버 시작
func (s *Server) Start() error {
	// 라우터 설정
	http.HandleFunc("/", s.handleDashboard)
	http.HandleFunc("/api/stats", s.handleStats)
	http.HandleFunc("/api/signals", s.handleSignals)
	http.HandleFunc("/api/orderbooks", s.handleOrderbooks)
	http.HandleFunc("/api/health", s.handleHealth)
	http.HandleFunc("/api/restart", s.handleRestart)
	http.HandleFunc("/api/stop", s.handleStop)
	
	// 서버 시작
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🌐 HTTP 서버 시작: http://localhost%s", addr)
	
	return http.ListenAndServe(addr, nil)
}

// handleDashboard 대시보드 핸들러
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	// CORS 헤더 설정
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	
	// 대시보드 HTML 반환
	html := s.generateDashboardHTML()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// handleStats 통계 API 핸들러
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	stats := s.getStats()
	json.NewEncoder(w).Encode(stats)
}

// handleSignals 시그널 API 핸들러
func (s *Server) handleSignals(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	limit := 10 // 기본값
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	
	signals := s.memManager.GetRecentSignals(limit)
	json.NewEncoder(w).Encode(signals)
}

// handleOrderbooks 오더북 API 핸들러
func (s *Server) handleOrderbooks(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	exchange := r.URL.Query().Get("exchange")
	symbol := r.URL.Query().Get("symbol")
	
	if exchange == "" || symbol == "" {
		http.Error(w, "exchange와 symbol 파라미터가 필요합니다", http.StatusBadRequest)
		return
	}
	
	duration := 5 * time.Minute // 기본값
	if durationStr := r.URL.Query().Get("duration"); durationStr != "" {
		if d, err := time.ParseDuration(durationStr); err == nil {
			duration = d
		}
	}
	
	orderbooks := s.memManager.GetRecentOrderbooks(exchange, symbol, duration)
	json.NewEncoder(w).Encode(orderbooks)
}

// handleHealth 헬스체크 핸들러
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	health := s.sysMonitor.GetHealthStatus()
	json.NewEncoder(w).Encode(health)
}

// handleRestart 재시작 핸들러
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	if r.Method != "POST" {
		http.Error(w, "POST 메서드만 지원합니다", http.StatusMethodNotAllowed)
		return
	}
	
	// 재시작 로직 (실제 구현에서는 더 안전한 방법 사용)
	s.sysMonitor.RecordRestart()
	
	response := map[string]interface{}{
		"status":  "success",
		"message": "재시작 요청이 처리되었습니다",
		"time":    time.Now(),
	}
	
	json.NewEncoder(w).Encode(response)
}

// handleStop 정지 핸들러
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.recordRequest()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	if r.Method != "POST" {
		http.Error(w, "POST 메서드만 지원합니다", http.StatusMethodNotAllowed)
		return
	}
	
	// 정지 로직 (실제 구현에서는 더 안전한 방법 사용)
	response := map[string]interface{}{
		"status":  "success",
		"message": "정지 요청이 처리되었습니다",
		"time":    time.Now(),
	}
	
	json.NewEncoder(w).Encode(response)
	
	// 3초 후 프로그램 종료
	go func() {
		time.Sleep(3 * time.Second)
		log.Fatal("프로그램 정지 요청으로 인한 종료")
	}()
}

// recordRequest 요청 기록
func (s *Server) recordRequest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.requestCount++
	s.lastRequestTime = time.Now()
}

// getStats 통계 조회
func (s *Server) getStats() map[string]interface{} {
	stats := make(map[string]interface{})
	
	// 메모리 통계
	memStats := s.memManager.GetMemoryStats()
	stats["memory"] = memStats
	
	// 워커 풀 통계
	if s.wsManager != nil {
		workerStats := s.wsManager.GetWorkerPoolStats()
		stats["workers"] = workerStats
	}
	
	// 성능 통계
	if s.perfMonitor != nil {
		perfStats := s.perfMonitor.GetStats()
		stats["performance"] = perfStats
	}
	
	// 시스템 통계
	if s.sysMonitor != nil {
		sysStats := s.sysMonitor.GetHealthStatus()
		stats["system"] = sysStats
	}
	
	// 서버 통계
	s.mu.RLock()
	serverStats := map[string]interface{}{
		"uptime":           time.Since(s.startTime).String(),
		"request_count":    s.requestCount,
		"last_request":     s.lastRequestTime,
		"port":             s.port,
	}
	s.mu.RUnlock()
	stats["server"] = serverStats
	
	return stats
}

// generateDashboardHTML 대시보드 HTML 생성
func (s *Server) generateDashboardHTML() string {
	return `<!DOCTYPE html>
<html lang="ko">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>펌핑 분석기 대시보드</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: #f5f5f5; }
        .container { max-width: 1400px; margin: 0 auto; padding: 20px; }
        .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 20px; border-radius: 10px; margin-bottom: 20px; }
        .header h1 { font-size: 2.5em; margin-bottom: 10px; }
        .header p { font-size: 1.1em; opacity: 0.9; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; margin-bottom: 20px; }
        .card { background: white; padding: 20px; border-radius: 10px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        .card h3 { color: #333; margin-bottom: 15px; font-size: 1.3em; }
        .metric { display: flex; justify-content: space-between; margin-bottom: 10px; padding: 10px; background: #f8f9fa; border-radius: 5px; }
        .metric .label { font-weight: 600; color: #555; }
        .metric .value { font-weight: bold; color: #007bff; }
        .status { padding: 5px 10px; border-radius: 15px; font-size: 0.9em; font-weight: bold; }
        .status.healthy { background: #d4edda; color: #155724; }
        .status.warning { background: #fff3cd; color: #856404; }
        .status.critical { background: #f8d7da; color: #721c24; }
        .signals { max-height: 400px; overflow-y: auto; }
        .signal-item { padding: 10px; margin-bottom: 10px; background: #f8f9fa; border-radius: 5px; border-left: 4px solid #007bff; }
        .signal-item .symbol { font-weight: bold; color: #333; }
        .signal-item .score { color: #007bff; font-weight: bold; }
        .signal-item .time { color: #666; font-size: 0.9em; }
        .controls { display: flex; gap: 10px; margin-top: 20px; }
        .btn { padding: 10px 20px; border: none; border-radius: 5px; cursor: pointer; font-weight: bold; transition: all 0.3s; }
        .btn-primary { background: #007bff; color: white; }
        .btn-primary:hover { background: #0056b3; }
        .btn-warning { background: #ffc107; color: #212529; }
        .btn-warning:hover { background: #e0a800; }
        .btn-danger { background: #dc3545; color: white; }
        .btn-danger:hover { background: #c82333; }
        .refresh-btn { position: fixed; top: 20px; right: 20px; background: #28a745; color: white; border: none; border-radius: 50%; width: 50px; height: 50px; cursor: pointer; font-size: 1.2em; }
        .refresh-btn:hover { background: #218838; }
        @media (max-width: 768px) { .grid { grid-template-columns: 1fr; } .header h1 { font-size: 2em; } }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🚀 펌핑 분석기 대시보드</h1>
            <p>실시간 암호화폐 펌핑 시그널 모니터링 시스템</p>
        </div>
        
        <button class="refresh-btn" onclick="refreshData()">🔄</button>
        
        <div class="grid">
            <div class="card">
                <h3>📊 시스템 상태</h3>
                <div id="system-status">로딩 중...</div>
            </div>
            
            <div class="card">
                <h3>⚡ 성능 지표</h3>
                <div id="performance-metrics">로딩 중...</div>
            </div>
            
            <div class="card">
                <h3>🔧 워커 풀</h3>
                <div id="worker-stats">로딩 중...</div>
            </div>
            
            <div class="card">
                <h3>💾 메모리 사용량</h3>
                <div id="memory-stats">로딩 중...</div>
            </div>
        </div>
        
        <div class="card">
            <h3>🚨 최근 시그널</h3>
            <div id="recent-signals" class="signals">로딩 중...</div>
        </div>
        
        <div class="controls">
            <button class="btn btn-primary" onclick="restartSystem()">🔄 재시작</button>
            <button class="btn btn-warning" onclick="testNotification()">📤 알림 테스트</button>
            <button class="btn btn-danger" onclick="stopSystem()">⏹️ 정지</button>
        </div>
    </div>

    <script>
        let refreshInterval;
        
        function refreshData() {
            fetch('/api/stats')
                .then(response => response.json())
                .then(data => {
                    updateSystemStatus(data.system);
                    updatePerformanceMetrics(data.performance);
                    updateWorkerStats(data.workers);
                    updateMemoryStats(data.memory);
                })
                .catch(error => console.error('통계 조회 실패:', error));
                
            fetch('/api/signals?limit=10')
                .then(response => response.json())
                .then(data => updateRecentSignals(data))
                .catch(error => console.error('시그널 조회 실패:', error));
        }
        
        function updateSystemStatus(system) {
            if (!system) return;
            
            const statusClass = system.health_status === 'HEALTHY' ? 'healthy' : 
                               system.health_status === 'WARNING' ? 'warning' : 'critical';
            
            document.getElementById('system-status').innerHTML = `
                <div class="metric">
                    <span class="label">상태:</span>
                    <span class="status ${statusClass}">${system.health_status}</span>
                </div>
                <div class="metric">
                    <span class="label">가동시간:</span>
                    <span class="value">${system.uptime}</span>
                </div>
                <div class="metric">
                    <span class="label">에러 수:</span>
                    <span class="value">${system.error_count}</span>
                </div>
                <div class="metric">
                    <span class="label">경고 수:</span>
                    <span class="value">${system.warning_count}</span>
                </div>
                <div class="metric">
                    <span class="label">재시작 수:</span>
                    <span class="value">${system.restart_count}</span>
                </div>
            `;
        }
        
        function updatePerformanceMetrics(performance) {
            if (!performance) return;
            
            const avgTime = performance.avg_processing_time ? performance.avg_processing_time.toFixed(2) : 0;
            
            document.getElementById('performance-metrics').innerHTML = 
                '<div class="metric">' +
                    '<span class="label">최대 처리량:</span>' +
                    '<span class="value">' + performance.peak_throughput + '/초</span>' +
                '</div>' +
                '<div class="metric">' +
                    '<span class="label">평균 처리량:</span>' +
                    '<span class="value">' + performance.average_throughput + '/초</span>' +
                '</div>' +
                '<div class="metric">' +
                    '<span class="label">오버플로우:</span>' +
                    '<span class="value">' + performance.overflow_count + '회</span>' +
                '</div>' +
                '<div class="metric">' +
                    '<span class="label">지연:</span>' +
                    '<span class="value">' + performance.delay_count + '회</span>' +
                '</div>' +
                '<div class="metric">' +
                    '<span class="label">평균 처리시간:</span>' +
                    '<span class="value">' + avgTime + 'ms</span>' +
                '</div>';
        }
        
        function updateWorkerStats(workers) {
            if (!workers) return;
            
            document.getElementById('worker-stats').innerHTML = `
                <div class="metric">
                    <span class="label">총 워커:</span>
                    <span class="value">${workers.worker_count}</span>
                </div>
                <div class="metric">
                    <span class="label">활성 워커:</span>
                    <span class="value">${workers.active_workers}</span>
                </div>
                <div class="metric">
                    <span class="label">데이터 버퍼:</span>
                    <span class="value">${workers.data_channel_buffer}/${workers.data_channel_capacity}</span>
                </div>
            `;
        }
        
        function updateMemoryStats(memory) {
            if (!memory) return;
            
            document.getElementById('memory-stats').innerHTML = `
                <div class="metric">
                    <span class="label">총 오더북:</span>
                    <span class="value">${memory.total_orderbooks}</span>
                </div>
                <div class="metric">
                    <span class="label">총 시그널:</span>
                    <span class="value">${memory.total_signals}</span>
                </div>
                <div class="metric">
                    <span class="label">보관 시간:</span>
                    <span class="value">${memory.retention_minutes}분</span>
                </div>
            `;
        }
        
        function updateRecentSignals(signals) {
            if (!signals || signals.length === 0) {
                document.getElementById('recent-signals').innerHTML = '<p>최근 시그널이 없습니다.</p>';
                return;
            }
            
            const signalsHtml = signals.map(function(signal) {
                const score = signal.composite_score ? signal.composite_score.toFixed(2) : 
                             signal.score ? signal.score.toFixed(2) : 0;
                const time = new Date(signal.timestamp).toLocaleString();
                
                return '<div class="signal-item">' +
                    '<div class="symbol">' + signal.symbol + '</div>' +
                    '<div class="score">점수: ' + score + '</div>' +
                    '<div class="time">' + time + '</div>' +
                '</div>';
            }).join('');
            
            document.getElementById('recent-signals').innerHTML = signalsHtml;
        }
        
        function restartSystem() {
            if (confirm('시스템을 재시작하시겠습니까?')) {
                fetch('/api/restart', { method: 'POST' })
                    .then(response => response.json())
                    .then(data => alert(data.message))
                    .catch(error => console.error('재시작 실패:', error));
            }
        }
        
        function testNotification() {
            fetch('/api/test-notification', { method: 'POST' })
                .then(response => response.json())
                .then(data => alert('알림 테스트가 전송되었습니다.'))
                .catch(error => console.error('알림 테스트 실패:', error));
        }
        
        function stopSystem() {
            if (confirm('정말로 시스템을 정지하시겠습니까?')) {
                fetch('/api/stop', { method: 'POST' })
                    .then(response => response.json())
                    .then(data => alert(data.message))
                    .catch(error => console.error('정지 실패:', error));
            }
        }
        
        // 초기 로드
        refreshData();
        
        // 5초마다 자동 새로고침
        refreshInterval = setInterval(refreshData, 5000);
        
        // 페이지 언로드 시 인터벌 정리
        window.addEventListener('beforeunload', () => {
            if (refreshInterval) clearInterval(refreshInterval);
        });
    </script>
</body>
</html>`
} 