import requests
import pandas as pd
import numpy as np
from datetime import datetime, timedelta
import time
import json
from typing import List, Dict, Optional

class BinanceTradeCollector:
    def __init__(self):
        self.base_url = "https://api.binance.com/api/v3"
        self.headers = {
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
        }
    
    def get_symbol_info(self, symbol: str) -> Optional[Dict]:
        """심볼 정보 조회 (상장 여부 확인)"""
        url = f"{self.base_url}/exchangeInfo"
        params = {'symbol': f"{symbol}USDT"}
        
        try:
            response = requests.get(url, headers=self.headers, params=params)
            response.raise_for_status()
            data = response.json()
            
            if 'symbols' in data and len(data['symbols']) > 0:
                return data['symbols'][0]
            return None
        except Exception as e:
            print(f"바이낸스 심볼 정보 조회 오류 ({symbol}): {e}")
            return None
    
    def get_historical_trades(self, symbol: str, start_time: datetime, end_time: datetime) -> List[Dict]:
        """특정 시간 범위의 체결 데이터 조회"""
        url = f"{self.base_url}/aggTrades"
        
        # 밀리초 단위 타임스탬프 변환
        start_ms = int(start_time.timestamp() * 1000)
        end_ms = int(end_time.timestamp() * 1000)
        
        params = {
            'symbol': f"{symbol}USDT",
            'startTime': start_ms,
            'endTime': end_ms,
            'limit': 1000  # 최대 1000개
        }
        
        try:
            response = requests.get(url, headers=self.headers, params=params)
            response.raise_for_status()
            trades = response.json()
            
            # 데이터 형식 변환
            processed_trades = []
            for trade in trades:
                processed_trade = {
                    'exchange': 'binance',
                    'symbol': symbol,
                    'trade_id': trade['a'],  # Aggregate trade ID
                    'price': float(trade['p']),
                    'quantity': float(trade['q']),
                    'timestamp': datetime.fromtimestamp(trade['T'] / 1000),
                    'is_buyer_maker': trade['m']  # True면 매도자가 maker
                }
                processed_trades.append(processed_trade)
            
            return processed_trades
        
        except Exception as e:
            print(f"바이낸스 체결 데이터 조회 오류 ({symbol}): {e}")
            return []

class BybitTradeCollector:
    def __init__(self):
        self.base_url = "https://api.bybit.com/v5"
        self.headers = {
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
        }
    
    def get_symbol_info(self, symbol: str) -> Optional[Dict]:
        """심볼 정보 조회 (상장 여부 확인)"""
        url = f"{self.base_url}/market/instruments-info"
        params = {
            'category': 'spot',
            'symbol': f"{symbol}USDT"
        }
        
        try:
            response = requests.get(url, headers=self.headers, params=params)
            response.raise_for_status()
            data = response.json()
            
            if data.get('retCode') == 0 and data.get('result', {}).get('list'):
                return data['result']['list'][0]
            return None
        except Exception as e:
            print(f"바이비트 심볼 정보 조회 오류 ({symbol}): {e}")
            return None
    
    def get_historical_trades(self, symbol: str, start_time: datetime, end_time: datetime) -> List[Dict]:
        """특정 시간 범위의 체결 데이터 조회"""
        url = f"{self.base_url}/market/recent-trade"
        
        params = {
            'category': 'spot',
            'symbol': f"{symbol}USDT",
            'limit': 1000
        }
        
        try:
            response = requests.get(url, headers=self.headers, params=params)
            response.raise_for_status()
            data = response.json()
            
            if data.get('retCode') != 0:
                print(f"바이비트 API 오류: {data.get('retMsg')}")
                return []
            
            trades = data.get('result', {}).get('list', [])
            
            # 시간 범위 필터링 및 데이터 형식 변환
            processed_trades = []
            for trade in trades:
                trade_time = datetime.fromtimestamp(int(trade['time']) / 1000)
                
                # 시간 범위 확인
                if start_time <= trade_time <= end_time:
                    processed_trade = {
                        'exchange': 'bybit',
                        'symbol': symbol,
                        'trade_id': trade['execId'],
                        'price': float(trade['price']),
                        'quantity': float(trade['size']),
                        'timestamp': trade_time,
                        'is_buyer_maker': trade['side'] == 'Sell'  # Sell이면 매도자가 maker
                    }
                    processed_trades.append(processed_trade)
            
            return processed_trades
        
        except Exception as e:
            print(f"바이비트 체결 데이터 조회 오류 ({symbol}): {e}")
            return []

class TradeDataAnalyzer:
    def __init__(self):
        self.binance_collector = BinanceTradeCollector()
        self.bybit_collector = BybitTradeCollector()
    
    def collect_pump_data(self, symbol: str, notice_time: datetime, duration_minutes: int = 1) -> Dict:
        """상장 공지 시점부터 지정된 시간 동안의 체결 데이터 수집"""
        
        # 시간 범위 설정
        start_time = notice_time
        end_time = notice_time + timedelta(minutes=duration_minutes)
        
        print(f"\n{symbol} 체결 데이터 수집 중...")
        print(f"시간 범위: {start_time} ~ {end_time}")
        
        # 바이낸스 데이터 수집
        print("바이낸스 데이터 수집 중...")
        binance_trades = self.binance_collector.get_historical_trades(symbol, start_time, end_time)
        time.sleep(0.2)  # API 제한 고려
        
        # 바이비트 데이터 수집
        print("바이비트 데이터 수집 중...")
        bybit_trades = self.bybit_collector.get_historical_trades(symbol, start_time, end_time)
        time.sleep(0.2)  # API 제한 고려
        
        # 결과 정리
        result = {
            'symbol': symbol,
            'notice_time': notice_time,
            'duration_minutes': duration_minutes,
            'binance_trades': binance_trades,
            'bybit_trades': bybit_trades,
            'binance_trade_count': len(binance_trades),
            'bybit_trade_count': len(bybit_trades)
        }
        
        print(f"바이낸스: {len(binance_trades)}개 체결")
        print(f"바이비트: {len(bybit_trades)}개 체결")
        
        return result
    
    def analyze_pump_pattern(self, trades_data: List[Dict]) -> Dict:
        """체결 데이터에서 펌핑 패턴 분석"""
        if not trades_data:
            return {}
        
        df = pd.DataFrame(trades_data)
        df = df.sort_values('timestamp')
        
        # 기본 통계
        analysis = {
            'total_trades': len(df),
            'total_volume': df['quantity'].sum(),
            'total_value': (df['price'] * df['quantity']).sum(),
            'price_min': df['price'].min(),
            'price_max': df['price'].max(),
            'price_first': df['price'].iloc[0] if len(df) > 0 else 0,
            'price_last': df['price'].iloc[-1] if len(df) > 0 else 0,
        }
        
        # 가격 변동률 계산
        if analysis['price_first'] > 0:
            analysis['price_change_pct'] = ((analysis['price_last'] - analysis['price_first']) / analysis['price_first']) * 100
            analysis['max_pump_pct'] = ((analysis['price_max'] - analysis['price_first']) / analysis['price_first']) * 100
        else:
            analysis['price_change_pct'] = 0
            analysis['max_pump_pct'] = 0
        
        # 시간대별 거래량 분석 (10초 간격)
        df['time_bucket'] = df['timestamp'].dt.floor('10S')
        volume_by_time = df.groupby('time_bucket').agg({
            'quantity': 'sum',
            'price': ['min', 'max', 'first', 'last']
        }).round(8)
        
        analysis['volume_by_time'] = volume_by_time.to_dict()
        
        return analysis
    
    def save_trades_to_csv(self, pump_data: Dict, filename_prefix: str = "pump_analysis"):
        """체결 데이터를 CSV로 저장"""
        symbol = pump_data['symbol']
        notice_time_str = pump_data['notice_time'].strftime('%Y%m%d_%H%M%S')
        
        # 바이낸스 데이터 저장
        if pump_data['binance_trades']:
            binance_df = pd.DataFrame(pump_data['binance_trades'])
            binance_filename = f"{filename_prefix}_{symbol}_binance_{notice_time_str}.csv"
            binance_df.to_csv(binance_filename, index=False, encoding='utf-8-sig')
            print(f"바이낸스 데이터 저장: {binance_filename}")
        
        # 바이비트 데이터 저장
        if pump_data['bybit_trades']:
            bybit_df = pd.DataFrame(pump_data['bybit_trades'])
            bybit_filename = f"{filename_prefix}_{symbol}_bybit_{notice_time_str}.csv"
            bybit_df.to_csv(bybit_filename, index=False, encoding='utf-8-sig')
            print(f"바이비트 데이터 저장: {bybit_filename}")

def main():
    """메인 실행 함수 - 테스트용"""
    analyzer = TradeDataAnalyzer()
    
    # 테스트: 특정 시간대의 BTC 데이터 수집
    test_symbol = "BTC"
    test_time = datetime.now() - timedelta(hours=1)  # 1시간 전
    
    print("=== 체결 데이터 수집 테스트 ===")
    pump_data = analyzer.collect_pump_data(test_symbol, test_time, duration_minutes=1)
    
    # 분석 결과 출력
    if pump_data['binance_trades']:
        binance_analysis = analyzer.analyze_pump_pattern(pump_data['binance_trades'])
        print(f"\n바이낸스 {test_symbol} 분석 결과:")
        print(f"총 체결 수: {binance_analysis.get('total_trades', 0)}")
        print(f"총 거래량: {binance_analysis.get('total_volume', 0):.8f}")
        print(f"가격 변동: {binance_analysis.get('price_change_pct', 0):.2f}%")
    
    if pump_data['bybit_trades']:
        bybit_analysis = analyzer.analyze_pump_pattern(pump_data['bybit_trades'])
        print(f"\n바이비트 {test_symbol} 분석 결과:")
        print(f"총 체결 수: {bybit_analysis.get('total_trades', 0)}")
        print(f"총 거래량: {bybit_analysis.get('total_volume', 0):.8f}")
        print(f"가격 변동: {bybit_analysis.get('price_change_pct', 0):.2f}%")
    
    # CSV 저장
    analyzer.save_trades_to_csv(pump_data)

if __name__ == "__main__":
    main() 