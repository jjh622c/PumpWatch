import matplotlib.pyplot as plt
import matplotlib.dates as mdates
import pandas as pd
import numpy as np
from datetime import datetime, timedelta
import seaborn as sns
from typing import List, Dict

# 한글 폰트 설정 (Windows 환경)
plt.rcParams['font.family'] = 'Malgun Gothic'
plt.rcParams['axes.unicode_minus'] = False

class PumpVisualizer:
    def __init__(self):
        # 스타일 설정
        plt.style.use('seaborn-v0_8')
        sns.set_palette("husl")
        
    def visualize_exchange_comparison(self, pump_data: Dict, save_path: str = None):
        """거래소별 체결 데이터 비교 시각화"""
        symbol = pump_data['symbol']
        notice_time = pump_data['notice_time']
        
        # 데이터 준비
        binance_trades = pump_data.get('binance_trades', [])
        bybit_trades = pump_data.get('bybit_trades', [])
        
        # Figure 설정 (2x2 서브플롯)
        fig, axes = plt.subplots(2, 2, figsize=(16, 12))
        fig.suptitle(f'{symbol} 상장 공지 펌핑 분석 - {notice_time.strftime("%Y-%m-%d %H:%M:%S")}', 
                     fontsize=16, fontweight='bold')
        
        # 1. 가격 변동 비교 (좌상단)
        self._plot_price_comparison(axes[0, 0], binance_trades, bybit_trades, notice_time)
        
        # 2. 거래량 비교 (우상단)
        self._plot_volume_comparison(axes[0, 1], binance_trades, bybit_trades, notice_time)
        
        # 3. 시간별 체결 빈도 (좌하단)
        self._plot_trade_frequency(axes[1, 0], binance_trades, bybit_trades, notice_time)
        
        # 4. 가격-거래량 산점도 (우하단)
        self._plot_price_volume_scatter(axes[1, 1], binance_trades, bybit_trades)
        
        plt.tight_layout()
        
        if save_path:
            plt.savefig(save_path, dpi=300, bbox_inches='tight')
            print(f"그래프 저장: {save_path}")
        
        plt.show()
    
    def _plot_price_comparison(self, ax, binance_trades, bybit_trades, notice_time):
        """가격 변동 비교 그래프"""
        ax.set_title('거래소별 가격 변동', fontweight='bold')
        
        # 바이낸스 데이터
        if binance_trades:
            binance_df = pd.DataFrame(binance_trades)
            binance_df = binance_df.sort_values('timestamp')
            ax.plot(binance_df['timestamp'], binance_df['price'], 
                   'b-', linewidth=1.5, alpha=0.8, label='바이낸스')
        
        # 바이비트 데이터
        if bybit_trades:
            bybit_df = pd.DataFrame(bybit_trades)
            bybit_df = bybit_df.sort_values('timestamp')
            ax.plot(bybit_df['timestamp'], bybit_df['price'], 
                   'r-', linewidth=1.5, alpha=0.8, label='바이비트')
        
        # 공지 시점 표시
        ax.axvline(x=notice_time, color='orange', linestyle='--', linewidth=2, 
                  label='상장 공지 시점')
        
        ax.set_xlabel('시간')
        ax.set_ylabel('가격 (USDT)')
        ax.legend()
        ax.grid(True, alpha=0.3)
        
        # 시간 축 포맷
        ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        ax.tick_params(axis='x', rotation=45)
    
    def _plot_volume_comparison(self, ax, binance_trades, bybit_trades, notice_time):
        """거래량 비교 그래프"""
        ax.set_title('거래소별 체결량', fontweight='bold')
        
        # 10초 간격으로 그룹화
        def group_by_time(trades, interval_seconds=10):
            if not trades:
                return pd.DataFrame()
            
            df = pd.DataFrame(trades)
            df['time_bucket'] = df['timestamp'].dt.floor(f'{interval_seconds}S')
            return df.groupby('time_bucket')['quantity'].sum().reset_index()
        
        # 바이낸스 데이터
        binance_grouped = group_by_time(binance_trades)
        if not binance_grouped.empty:
            ax.bar(binance_grouped['time_bucket'], binance_grouped['quantity'], 
                  width=timedelta(seconds=8), alpha=0.7, label='바이낸스', color='blue')
        
        # 바이비트 데이터
        bybit_grouped = group_by_time(bybit_trades)
        if not bybit_grouped.empty:
            ax.bar(bybit_grouped['time_bucket'], bybit_grouped['quantity'], 
                  width=timedelta(seconds=8), alpha=0.7, label='바이비트', color='red')
        
        # 공지 시점 표시
        ax.axvline(x=notice_time, color='orange', linestyle='--', linewidth=2, 
                  label='상장 공지 시점')
        
        ax.set_xlabel('시간')
        ax.set_ylabel('체결량')
        ax.legend()
        ax.grid(True, alpha=0.3)
        
        # 시간 축 포맷
        ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        ax.tick_params(axis='x', rotation=45)
    
    def _plot_trade_frequency(self, ax, binance_trades, bybit_trades, notice_time):
        """시간별 체결 빈도"""
        ax.set_title('시간별 체결 빈도', fontweight='bold')
        
        # 5초 간격으로 체결 수 계산
        def get_trade_frequency(trades, interval_seconds=5):
            if not trades:
                return pd.DataFrame()
            
            df = pd.DataFrame(trades)
            df['time_bucket'] = df['timestamp'].dt.floor(f'{interval_seconds}S')
            return df.groupby('time_bucket').size().reset_index(name='trade_count')
        
        # 바이낸스 데이터
        binance_freq = get_trade_frequency(binance_trades)
        if not binance_freq.empty:
            ax.plot(binance_freq['time_bucket'], binance_freq['trade_count'], 
                   'b-o', markersize=4, label='바이낸스', linewidth=2)
        
        # 바이비트 데이터
        bybit_freq = get_trade_frequency(bybit_trades)
        if not bybit_freq.empty:
            ax.plot(bybit_freq['time_bucket'], bybit_freq['trade_count'], 
                   'r-s', markersize=4, label='바이비트', linewidth=2)
        
        # 공지 시점 표시
        ax.axvline(x=notice_time, color='orange', linestyle='--', linewidth=2, 
                  label='상장 공지 시점')
        
        ax.set_xlabel('시간')
        ax.set_ylabel('체결 횟수 (5초 간격)')
        ax.legend()
        ax.grid(True, alpha=0.3)
        
        # 시간 축 포맷
        ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        ax.tick_params(axis='x', rotation=45)
    
    def _plot_price_volume_scatter(self, ax, binance_trades, bybit_trades):
        """가격-거래량 산점도"""
        ax.set_title('가격 vs 체결량 산점도', fontweight='bold')
        
        # 바이낸스 데이터
        if binance_trades:
            binance_df = pd.DataFrame(binance_trades)
            ax.scatter(binance_df['price'], binance_df['quantity'], 
                      alpha=0.6, s=30, label='바이낸스', color='blue')
        
        # 바이비트 데이터
        if bybit_trades:
            bybit_df = pd.DataFrame(bybit_trades)
            ax.scatter(bybit_df['price'], bybit_df['quantity'], 
                      alpha=0.6, s=30, label='바이비트', color='red')
        
        ax.set_xlabel('가격 (USDT)')
        ax.set_ylabel('체결량')
        ax.legend()
        ax.grid(True, alpha=0.3)
    
    def create_detailed_analysis_report(self, pump_data: Dict) -> Dict:
        """상세 분석 리포트 생성"""
        symbol = pump_data['symbol']
        binance_trades = pump_data.get('binance_trades', [])
        bybit_trades = pump_data.get('bybit_trades', [])
        
        report = {
            'symbol': symbol,
            'notice_time': pump_data['notice_time'],
            'analysis_summary': {},
            'binance_analysis': self._analyze_exchange_data(binance_trades, 'binance'),
            'bybit_analysis': self._analyze_exchange_data(bybit_trades, 'bybit')
        }
        
        # 거래소 간 비교 분석
        if binance_trades and bybit_trades:
            report['comparison'] = self._compare_exchanges(binance_trades, bybit_trades)
        
        return report
    
    def _analyze_exchange_data(self, trades: List[Dict], exchange_name: str) -> Dict:
        """거래소별 데이터 분석"""
        if not trades:
            return {'message': f'{exchange_name} 데이터 없음'}
        
        df = pd.DataFrame(trades)
        df = df.sort_values('timestamp')
        
        analysis = {
            'total_trades': len(df),
            'total_volume': df['quantity'].sum(),
            'total_value': (df['price'] * df['quantity']).sum(),
            'price_range': {
                'min': df['price'].min(),
                'max': df['price'].max(),
                'first': df['price'].iloc[0],
                'last': df['price'].iloc[-1]
            },
            'volume_stats': {
                'mean': df['quantity'].mean(),
                'median': df['quantity'].median(),
                'std': df['quantity'].std()
            }
        }
        
        # 가격 변동률 계산
        if analysis['price_range']['first'] > 0:
            price_change = ((analysis['price_range']['last'] - analysis['price_range']['first']) 
                          / analysis['price_range']['first']) * 100
            max_pump = ((analysis['price_range']['max'] - analysis['price_range']['first']) 
                       / analysis['price_range']['first']) * 100
            
            analysis['price_change_pct'] = price_change
            analysis['max_pump_pct'] = max_pump
        
        return analysis
    
    def _compare_exchanges(self, binance_trades: List[Dict], bybit_trades: List[Dict]) -> Dict:
        """거래소 간 비교 분석"""
        binance_df = pd.DataFrame(binance_trades)
        bybit_df = pd.DataFrame(bybit_trades)
        
        comparison = {
            'trade_count_diff': len(binance_df) - len(bybit_df),
            'volume_diff': binance_df['quantity'].sum() - bybit_df['quantity'].sum(),
        }
        
        # 평균 가격 차이
        if len(binance_df) > 0 and len(bybit_df) > 0:
            binance_avg_price = (binance_df['price'] * binance_df['quantity']).sum() / binance_df['quantity'].sum()
            bybit_avg_price = (bybit_df['price'] * bybit_df['quantity']).sum() / bybit_df['quantity'].sum()
            
            comparison['avg_price_diff'] = binance_avg_price - bybit_avg_price
            comparison['avg_price_diff_pct'] = ((binance_avg_price - bybit_avg_price) / bybit_avg_price) * 100
        
        return comparison

def main():
    """메인 함수 - 시각화 테스트"""
    # 테스트 데이터 생성 (실제로는 exchange_trade_collector.py에서 가져옴)
    test_notice_time = datetime.now() - timedelta(minutes=5)
    
    # 샘플 데이터
    test_pump_data = {
        'symbol': 'TEST',
        'notice_time': test_notice_time,
        'duration_minutes': 1,
        'binance_trades': [
            {
                'exchange': 'binance',
                'symbol': 'TEST',
                'price': 100.0 + i * 0.5,
                'quantity': 10.0 + np.random.normal(0, 2),
                'timestamp': test_notice_time + timedelta(seconds=i*2)
            }
            for i in range(30)
        ],
        'bybit_trades': [
            {
                'exchange': 'bybit',
                'symbol': 'TEST',
                'price': 100.2 + i * 0.3,
                'quantity': 8.0 + np.random.normal(0, 1.5),
                'timestamp': test_notice_time + timedelta(seconds=i*3)
            }
            for i in range(20)
        ]
    }
    
    # 시각화 테스트
    visualizer = PumpVisualizer()
    
    print("=== 펌핑 시각화 테스트 ===")
    visualizer.visualize_exchange_comparison(test_pump_data, "test_pump_analysis.png")
    
    # 분석 리포트 생성
    report = visualizer.create_detailed_analysis_report(test_pump_data)
    
    print("\n=== 분석 리포트 ===")
    print(f"심볼: {report['symbol']}")
    print(f"공지 시간: {report['notice_time']}")
    
    if 'binance_analysis' in report:
        ba = report['binance_analysis']
        print(f"\n바이낸스 분석:")
        print(f"  총 체결수: {ba.get('total_trades', 0)}")
        print(f"  총 거래량: {ba.get('total_volume', 0):.2f}")
        print(f"  가격 변동: {ba.get('price_change_pct', 0):.2f}%")
        print(f"  최대 펌핑: {ba.get('max_pump_pct', 0):.2f}%")
    
    if 'bybit_analysis' in report:
        bba = report['bybit_analysis']
        print(f"\n바이비트 분석:")
        print(f"  총 체결수: {bba.get('total_trades', 0)}")
        print(f"  총 거래량: {bba.get('total_volume', 0):.2f}")
        print(f"  가격 변동: {bba.get('price_change_pct', 0):.2f}%")
        print(f"  최대 펌핑: {bba.get('max_pump_pct', 0):.2f}%")

if __name__ == "__main__":
    main() 