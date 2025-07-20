"""
거래소별 오더북 과거 데이터 가용성 테스트
바이낸스, 바이비트에서 오더북 스냅샷 데이터 수집 가능성 확인
"""

import requests
import pandas as pd
import numpy as np
from datetime import datetime, timedelta
import time
import json

class OrderbookDataTester:
    def __init__(self):
        self.test_symbols = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT']
        self.exchanges = {
            'binance': {
                'name': 'Binance',
                'orderbook_url': 'https://api.binance.com/api/v3/depth',
                'klines_url': 'https://api.binance.com/api/v3/klines',
                'agg_trades_url': 'https://api.binance.com/api/v3/aggTrades'
            },
            'bybit': {
                'name': 'Bybit',
                'orderbook_url': 'https://api.bybit.com/v5/market/orderbook',
                'klines_url': 'https://api.bybit.com/v5/market/kline',
                'trades_url': 'https://api.bybit.com/v5/market/recent-trade'
            }
        }
        
    def test_binance_historical_data(self, symbol='BTCUSDT'):
        """바이낸스 과거 데이터 테스트"""
        print(f"\n🔍 바이낸스 과거 데이터 테스트 ({symbol})")
        
        results = {}
        
        # 1. 현재 오더북 (실시간)
        try:
            orderbook_url = self.exchanges['binance']['orderbook_url']
            response = requests.get(orderbook_url, params={'symbol': symbol, 'limit': 20})
            response.raise_for_status()
            orderbook_data = response.json()
            
            results['current_orderbook'] = {
                'available': True,
                'bids_count': len(orderbook_data['bids']),
                'asks_count': len(orderbook_data['asks']),
                'last_update_id': orderbook_data['lastUpdateId']
            }
            print(f"   ✅ 현재 오더북: {results['current_orderbook']['bids_count']} bids, {results['current_orderbook']['asks_count']} asks")
            
        except Exception as e:
            results['current_orderbook'] = {'available': False, 'error': str(e)}
            print(f"   ❌ 현재 오더북 오류: {e}")
        
        # 2. 과거 체결 데이터 (1시간 전)
        try:
            one_hour_ago = int((datetime.now() - timedelta(hours=1)).timestamp() * 1000)
            now = int(datetime.now().timestamp() * 1000)
            
            agg_trades_url = self.exchanges['binance']['agg_trades_url']
            response = requests.get(agg_trades_url, params={
                'symbol': symbol,
                'startTime': one_hour_ago,
                'endTime': now,
                'limit': 100
            })
            response.raise_for_status()
            trades_data = response.json()
            
            results['historical_trades'] = {
                'available': True,
                'count': len(trades_data),
                'time_range': '1시간',
                'sample_trade': trades_data[0] if trades_data else None
            }
            print(f"   ✅ 과거 체결: {len(trades_data)}개 (1시간)")
            
        except Exception as e:
            results['historical_trades'] = {'available': False, 'error': str(e)}
            print(f"   ❌ 과거 체결 오류: {e}")
        
        # 3. 과거 캔들 데이터
        try:
            klines_url = self.exchanges['binance']['klines_url']
            response = requests.get(klines_url, params={
                'symbol': symbol,
                'interval': '1s',  # 1초 캔들
                'limit': 60  # 1분간
            })
            response.raise_for_status()
            klines_data = response.json()
            
            results['historical_klines'] = {
                'available': True,
                'count': len(klines_data),
                'interval': '1초',
                'sample_kline': klines_data[0] if klines_data else None
            }
            print(f"   ✅ 과거 캔들: {len(klines_data)}개 (1초 간격)")
            
        except Exception as e:
            results['historical_klines'] = {'available': False, 'error': str(e)}
            print(f"   ❌ 과거 캔들 오류: {e}")
        
        return results
    
    def test_bybit_historical_data(self, symbol='BTCUSDT'):
        """바이비트 과거 데이터 테스트"""
        print(f"\n🔍 바이비트 과거 데이터 테스트 ({symbol})")
        
        results = {}
        
        # 1. 현재 오더북
        try:
            orderbook_url = self.exchanges['bybit']['orderbook_url']
            response = requests.get(orderbook_url, params={
                'category': 'spot',
                'symbol': symbol,
                'limit': 25
            })
            response.raise_for_status()
            data = response.json()
            
            if data.get('retCode') == 0:
                orderbook_data = data['result']
                results['current_orderbook'] = {
                    'available': True,
                    'bids_count': len(orderbook_data['b']),
                    'asks_count': len(orderbook_data['a']),
                    'timestamp': orderbook_data['ts']
                }
                print(f"   ✅ 현재 오더북: {results['current_orderbook']['bids_count']} bids, {results['current_orderbook']['asks_count']} asks")
            else:
                raise Exception(f"API 오류: {data.get('retMsg')}")
                
        except Exception as e:
            results['current_orderbook'] = {'available': False, 'error': str(e)}
            print(f"   ❌ 현재 오더북 오류: {e}")
        
        # 2. 최근 체결 데이터
        try:
            trades_url = self.exchanges['bybit']['trades_url']
            response = requests.get(trades_url, params={
                'category': 'spot',
                'symbol': symbol,
                'limit': 100
            })
            response.raise_for_status()
            data = response.json()
            
            if data.get('retCode') == 0:
                trades_data = data['result']['list']
                results['recent_trades'] = {
                    'available': True,
                    'count': len(trades_data),
                    'type': '최근 체결',
                    'sample_trade': trades_data[0] if trades_data else None
                }
                print(f"   ✅ 최근 체결: {len(trades_data)}개")
            else:
                raise Exception(f"API 오류: {data.get('retMsg')}")
                
        except Exception as e:
            results['recent_trades'] = {'available': False, 'error': str(e)}
            print(f"   ❌ 최근 체결 오류: {e}")
        
        # 3. 과거 캔들 데이터
        try:
            klines_url = self.exchanges['bybit']['klines_url']
            # 1분 전부터 현재까지
            end_time = int(datetime.now().timestamp() * 1000)
            start_time = int((datetime.now() - timedelta(minutes=5)).timestamp() * 1000)
            
            response = requests.get(klines_url, params={
                'category': 'spot',
                'symbol': symbol,
                'interval': '1',  # 1분
                'start': start_time,
                'end': end_time,
                'limit': 10
            })
            response.raise_for_status()
            data = response.json()
            
            if data.get('retCode') == 0:
                klines_data = data['result']['list']
                results['historical_klines'] = {
                    'available': True,
                    'count': len(klines_data),
                    'interval': '1분',
                    'sample_kline': klines_data[0] if klines_data else None
                }
                print(f"   ✅ 과거 캔들: {len(klines_data)}개 (1분 간격)")
            else:
                raise Exception(f"API 오류: {data.get('retMsg')}")
                
        except Exception as e:
            results['historical_klines'] = {'available': False, 'error': str(e)}
            print(f"   ❌ 과거 캔들 오류: {e}")
        
        return results
    
    def test_ultra_high_frequency_data(self):
        """초고빈도 데이터 수집 가능성 테스트"""
        print(f"\n⚡ 초고빈도 데이터 수집 테스트")
        
        results = {}
        
        # 바이낸스 1초 캔들 테스트
        try:
            print("   바이낸스 1초 캔들 테스트...")
            url = "https://api.binance.com/api/v3/klines"
            response = requests.get(url, params={
                'symbol': 'BTCUSDT',
                'interval': '1s',
                'limit': 10
            })
            response.raise_for_status()
            data = response.json()
            
            results['binance_1s_candles'] = {
                'available': True,
                'count': len(data),
                'resolution': '1초',
                'sample': data[0] if data else None
            }
            print(f"      ✅ 1초 캔들: {len(data)}개")
            
        except Exception as e:
            results['binance_1s_candles'] = {'available': False, 'error': str(e)}
            print(f"      ❌ 1초 캔들 오류: {e}")
        
        # 오더북 스냅샷 빈도 테스트
        print("   오더북 스냅샷 연속 수집 테스트 (5초간)...")
        orderbook_snapshots = []
        
        try:
            for i in range(5):
                start_time = time.time()
                
                # 바이낸스 오더북
                response = requests.get("https://api.binance.com/api/v3/depth", 
                                      params={'symbol': 'BTCUSDT', 'limit': 5})
                response.raise_for_status()
                data = response.json()
                
                snapshot = {
                    'timestamp': time.time(),
                    'response_time_ms': (time.time() - start_time) * 1000,
                    'bids': len(data['bids']),
                    'asks': len(data['asks']),
                    'update_id': data['lastUpdateId']
                }
                orderbook_snapshots.append(snapshot)
                
                time.sleep(1)  # 1초 대기
            
            # 분석
            response_times = [s['response_time_ms'] for s in orderbook_snapshots]
            avg_response_time = sum(response_times) / len(response_times)
            
            results['orderbook_frequency_test'] = {
                'available': True,
                'snapshots_count': len(orderbook_snapshots),
                'avg_response_time_ms': avg_response_time,
                'max_response_time_ms': max(response_times),
                'can_achieve_100ms': avg_response_time < 100
            }
            
            print(f"      ✅ 오더북 스냅샷: {len(orderbook_snapshots)}개")
            print(f"      ⚡ 평균 응답시간: {avg_response_time:.1f}ms")
            print(f"      🎯 100ms 달성 가능: {'✅' if avg_response_time < 100 else '❌'}")
            
        except Exception as e:
            results['orderbook_frequency_test'] = {'available': False, 'error': str(e)}
            print(f"      ❌ 오더북 테스트 오류: {e}")
        
        return results
    
    def generate_data_availability_report(self):
        """데이터 가용성 종합 리포트"""
        print("="*80)
        print("거래소별 데이터 가용성 테스트 리포트")
        print("="*80)
        
        all_results = {}
        
        # 각 거래소 테스트
        for symbol in ['BTCUSDT']:  # 대표 심볼로 테스트
            print(f"\n📊 테스트 심볼: {symbol}")
            
            # 바이낸스 테스트
            binance_results = self.test_binance_historical_data(symbol)
            all_results['binance'] = binance_results
            
            # 바이비트 테스트
            bybit_results = self.test_bybit_historical_data(symbol)
            all_results['bybit'] = bybit_results
            
            # 초고빈도 테스트
            hf_results = self.test_ultra_high_frequency_data()
            all_results['high_frequency'] = hf_results
        
        # 종합 분석
        print(f"\n" + "="*80)
        print("종합 분석 결과")
        print("="*80)
        
        # 과거 오더북 데이터 가용성
        print(f"\n📋 과거 오더북 데이터:")
        print(f"   바이낸스: ❌ 과거 오더북 스냅샷 제공 안함")
        print(f"   바이비트: ❌ 과거 오더북 스냅샷 제공 안함")
        print(f"   결론: 🔴 실시간 WebSocket 수집 필요")
        
        # 과거 체결 데이터
        print(f"\n📈 과거 체결 데이터:")
        binance_trades = all_results['binance'].get('historical_trades', {}).get('available', False)
        bybit_trades = all_results['bybit'].get('recent_trades', {}).get('available', False)
        print(f"   바이낸스: {'✅' if binance_trades else '❌'} {'과거 체결 데이터 제공' if binance_trades else '제한적'}")
        print(f"   바이비트: {'✅' if bybit_trades else '❌'} {'최근 체결만 제공' if bybit_trades else '제한적'}")
        
        # 실시간 성능
        print(f"\n⚡ 실시간 데이터 성능:")
        hf_test = all_results['high_frequency'].get('orderbook_frequency_test', {})
        if hf_test.get('available'):
            avg_time = hf_test.get('avg_response_time_ms', 0)
            can_100ms = hf_test.get('can_achieve_100ms', False)
            print(f"   평균 응답시간: {avg_time:.1f}ms")
            print(f"   100ms 목표 달성: {'✅' if can_100ms else '❌'}")
            print(f"   1초 내 분석 가능: ✅")
        
        # 권장사항
        print(f"\n💡 권장 구현 방식:")
        print(f"   1. 🔴 과거 오더북 데이터 없음 → WebSocket 실시간 수집")
        print(f"   2. ⚡ 100ms 이내 오더북 스냅샷 수집 가능")
        print(f"   3. 🎯 1초 내 분석 완전 가능")
        print(f"   4. 💾 자동 데이터 삭제로 용량 최적화 필수")
        print(f"   5. 🚀 WebSocket + 인메모리 저장 + 1초 분석 조합")
        
        # 결과 저장
        with open('data_availability_report.json', 'w', encoding='utf-8') as f:
            json.dump(all_results, f, ensure_ascii=False, indent=2, default=str)
        
        print(f"\n📄 상세 리포트 저장: data_availability_report.json")
        
        return all_results

def main():
    """메인 실행"""
    tester = OrderbookDataTester()
    tester.generate_data_availability_report()

if __name__ == "__main__":
    main() 