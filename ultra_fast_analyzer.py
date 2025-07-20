"""
1초 이내 초고속 펌핑 분석 시스템
상장 공지 후 1초 내 매수 시그널 감지
"""

import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
from datetime import datetime, timedelta
import json
import os
import time

# 한글 폰트 설정
plt.rcParams['font.family'] = 'Malgun Gothic'
plt.rcParams['axes.unicode_minus'] = False

class UltraFastPumpAnalyzer:
    def __init__(self):
        self.analysis_window = 1.0  # 1초 분석 윈도우
        self.signal_threshold = 5.0  # 5% 이상 상승 시 시그널
        self.ultra_signal_threshold = 10.0  # 10% 이상 시 강한 시그널
        
    def generate_ultra_fast_data(self, symbol, notice_time, pump_factor=1.0):
        """1초간의 초고속 체결/오더북 데이터 생성"""
        
        base_price = 100.0
        base_volume = 50.0  # 더 높은 기준 거래량
        
        # 100ms 간격으로 1초간 데이터 (10개 포인트)
        trades = []
        orderbook_snapshots = []
        current_price = base_price
        
        for i in range(10):  # 100ms * 10 = 1초
            timestamp = notice_time + timedelta(milliseconds=i*100)
            
            # 펌핑 효과: 첫 500ms에 급격한 상승
            if i < 5:  # 처음 500ms
                price_change = np.random.normal(1.0 * pump_factor, 0.3)
                volume_multiplier = 5 + pump_factor * 2
            else:  # 나중 500ms
                price_change = np.random.normal(0.2 * pump_factor, 0.2)
                volume_multiplier = 3 + pump_factor
            
            current_price += price_change
            volume = base_volume * volume_multiplier * np.random.uniform(0.8, 1.2)
            
            # 체결 데이터
            trade = {
                'timestamp': timestamp,
                'price': max(current_price, 0.1),
                'quantity': max(volume, 0.1),
                'side': 'buy' if np.random.random() > 0.3 else 'sell',  # 70% 매수 편향
                'millisecond': i * 100
            }
            trades.append(trade)
            
            # 오더북 스냅샷 (매수/매도 호가)
            spread = current_price * 0.001  # 0.1% 스프레드
            orderbook = {
                'timestamp': timestamp,
                'millisecond': i * 100,
                'bids': [  # 매수 호가 (높은 가격순)
                    {'price': current_price - spread, 'quantity': volume * 0.8},
                    {'price': current_price - spread * 2, 'quantity': volume * 1.2},
                    {'price': current_price - spread * 3, 'quantity': volume * 1.5}
                ],
                'asks': [  # 매도 호가 (낮은 가격순)
                    {'price': current_price + spread, 'quantity': volume * 0.6},
                    {'price': current_price + spread * 2, 'quantity': volume * 1.0},
                    {'price': current_price + spread * 3, 'quantity': volume * 1.3}
                ],
                'mid_price': current_price,
                'spread_bps': (spread * 2 / current_price) * 10000  # basis points
            }
            orderbook_snapshots.append(orderbook)
        
        return trades, orderbook_snapshots
    
    def analyze_ultra_fast_signal(self, trades, orderbook_snapshots):
        """1초 내 초고속 시그널 분석"""
        
        if not trades or not orderbook_snapshots:
            return {}
        
        trades_df = pd.DataFrame(trades)
        
        # 기본 분석
        price_first = trades_df['price'].iloc[0]
        price_last = trades_df['price'].iloc[-1]
        price_max = trades_df['price'].max()
        price_min = trades_df['price'].min()
        
        # 가격 변동률
        price_change_pct = ((price_last - price_first) / price_first) * 100
        max_pump_pct = ((price_max - price_first) / price_first) * 100
        
        # 거래량 분석
        total_volume = trades_df['quantity'].sum()
        buy_volume = trades_df[trades_df['side'] == 'buy']['quantity'].sum()
        sell_volume = trades_df[trades_df['side'] == 'sell']['quantity'].sum()
        buy_pressure = (buy_volume / total_volume) * 100 if total_volume > 0 else 0
        
        # 오더북 분석
        orderbook_df = pd.DataFrame(orderbook_snapshots)
        avg_spread = orderbook_df['spread_bps'].mean()
        spread_compression = (orderbook_df['spread_bps'].iloc[0] - orderbook_df['spread_bps'].iloc[-1]) / orderbook_df['spread_bps'].iloc[0] * 100
        
        # 시그널 점수 계산 (0-100)
        signal_score = 0
        signal_reasons = []
        
        # 가격 상승 점수 (40점 만점)
        if max_pump_pct >= self.ultra_signal_threshold:
            signal_score += 40
            signal_reasons.append(f"강한 펌핑: {max_pump_pct:.2f}%")
        elif max_pump_pct >= self.signal_threshold:
            signal_score += 25
            signal_reasons.append(f"펌핑 감지: {max_pump_pct:.2f}%")
        
        # 매수 압력 점수 (30점 만점)
        if buy_pressure >= 80:
            signal_score += 30
            signal_reasons.append(f"강한 매수압력: {buy_pressure:.1f}%")
        elif buy_pressure >= 70:
            signal_score += 20
            signal_reasons.append(f"매수압력: {buy_pressure:.1f}%")
        
        # 거래량 급증 점수 (20점 만점)
        volume_intensity = total_volume / len(trades)  # 평균 거래량
        if volume_intensity >= 100:
            signal_score += 20
            signal_reasons.append(f"거래량 급증: {volume_intensity:.1f}")
        elif volume_intensity >= 50:
            signal_score += 10
            signal_reasons.append(f"거래량 증가: {volume_intensity:.1f}")
        
        # 스프레드 압축 점수 (10점 만점)
        if spread_compression >= 20:
            signal_score += 10
            signal_reasons.append(f"스프레드 압축: {spread_compression:.1f}%")
        
        # 시그널 등급 결정
        if signal_score >= 80:
            signal_grade = "🔥 매우 강함"
            action = "즉시 매수"
        elif signal_score >= 60:
            signal_grade = "🚀 강함"
            action = "빠른 매수"
        elif signal_score >= 40:
            signal_grade = "⚡ 보통"
            action = "신중 매수"
        elif signal_score >= 20:
            signal_grade = "📊 약함"
            action = "관망"
        else:
            signal_grade = "😴 없음"
            action = "대기"
        
        return {
            'signal_score': signal_score,
            'signal_grade': signal_grade,
            'action': action,
            'signal_reasons': signal_reasons,
            'price_analysis': {
                'price_first': price_first,
                'price_last': price_last,
                'price_max': price_max,
                'price_change_pct': price_change_pct,
                'max_pump_pct': max_pump_pct
            },
            'volume_analysis': {
                'total_volume': total_volume,
                'buy_volume': buy_volume,
                'sell_volume': sell_volume,
                'buy_pressure': buy_pressure
            },
            'orderbook_analysis': {
                'avg_spread_bps': avg_spread,
                'spread_compression': spread_compression
            },
            'timing': {
                'analysis_window': '1초',
                'data_points': len(trades),
                'resolution': '100ms'
            }
        }
    
    def visualize_ultra_fast_data(self, symbol, notice_time, trades, orderbook_snapshots, analysis):
        """1초간 초고속 데이터 시각화"""
        
        fig, axes = plt.subplots(2, 2, figsize=(16, 12))
        
        signal_score = analysis['signal_score']
        signal_grade = analysis['signal_grade']
        
        fig.suptitle(f'{symbol} 초고속 펌핑 분석 (1초) - {signal_grade} (점수: {signal_score})\n'
                     f'{notice_time.strftime("%Y-%m-%d %H:%M:%S")}', 
                     fontsize=16, fontweight='bold')
        
        trades_df = pd.DataFrame(trades)
        
        # 1. 가격 변동 (100ms 단위)
        ax1 = axes[0, 0]
        ax1.plot(trades_df['millisecond'], trades_df['price'], 'b-o', linewidth=3, markersize=6)
        ax1.axvline(x=0, color='red', linestyle='--', linewidth=2, label='상장 공지')
        ax1.set_title('초단위 가격 변동 (100ms 해상도)')
        ax1.set_xlabel('시간 (ms)')
        ax1.set_ylabel('가격 (USDT)')
        ax1.legend()
        ax1.grid(True, alpha=0.3)
        
        # 2. 체결량 & 매수/매도 압력
        ax2 = axes[0, 1]
        buy_trades = trades_df[trades_df['side'] == 'buy']
        sell_trades = trades_df[trades_df['side'] == 'sell']
        
        ax2.bar(buy_trades['millisecond'], buy_trades['quantity'], 
               color='green', alpha=0.7, label='매수', width=80)
        ax2.bar(sell_trades['millisecond'], -sell_trades['quantity'], 
               color='red', alpha=0.7, label='매도', width=80)
        ax2.axhline(y=0, color='black', linestyle='-', alpha=0.5)
        ax2.set_title('매수/매도 압력')
        ax2.set_xlabel('시간 (ms)')
        ax2.set_ylabel('체결량 (매수+, 매도-)')
        ax2.legend()
        ax2.grid(True, alpha=0.3)
        
        # 3. 오더북 스프레드 변화
        ax3 = axes[1, 0]
        orderbook_df = pd.DataFrame(orderbook_snapshots)
        ax3.plot(orderbook_df['millisecond'], orderbook_df['spread_bps'], 
                'purple', linewidth=3, marker='s', markersize=6)
        ax3.set_title('스프레드 변화 (Basis Points)')
        ax3.set_xlabel('시간 (ms)')
        ax3.set_ylabel('스프레드 (bps)')
        ax3.grid(True, alpha=0.3)
        
        # 4. 시그널 점수 구성
        ax4 = axes[1, 1]
        categories = ['가격상승', '매수압력', '거래량', '스프레드']
        scores = [
            min(40, analysis['price_analysis']['max_pump_pct'] * 4),  # 대략적 점수
            analysis['volume_analysis']['buy_pressure'] * 0.3,
            min(20, analysis['volume_analysis']['total_volume'] / len(trades) * 0.2),
            min(10, abs(analysis['orderbook_analysis']['spread_compression']) * 0.5)
        ]
        
        colors = ['orange', 'green', 'blue', 'purple']
        bars = ax4.bar(categories, scores, color=colors, alpha=0.7)
        
        # 점수 표시
        for bar, score in zip(bars, scores):
            height = bar.get_height()
            ax4.text(bar.get_x() + bar.get_width()/2., height + 1,
                    f'{score:.1f}', ha='center', va='bottom', fontweight='bold')
        
        ax4.set_title('시그널 점수 구성')
        ax4.set_ylabel('점수')
        ax4.set_ylim(0, 50)
        ax4.grid(True, alpha=0.3)
        
        plt.tight_layout()
        
        # 저장
        os.makedirs('ultra_fast_results', exist_ok=True)
        filename = f"ultra_fast_results/{symbol}_{notice_time.strftime('%Y%m%d_%H%M%S')}_1sec.png"
        plt.savefig(filename, dpi=300, bbox_inches='tight')
        print(f"초고속 분석 차트 저장: {filename}")
        
        plt.show()
    
    def test_ultra_fast_analysis(self):
        """초고속 분석 테스트"""
        
        print("="*70)
        print("1초 이내 초고속 펌핑 분석 테스트")
        print("="*70)
        
        # 테스트 케이스들
        test_cases = [
            {"symbol": "SWELL", "pump_factor": 4.0, "description": "강한 펌핑"},
            {"symbol": "CTSI", "pump_factor": 2.0, "description": "보통 펌핑"},
            {"symbol": "PEPE", "pump_factor": 0.5, "description": "약한 변동"}
        ]
        
        results = []
        
        for case in test_cases:
            symbol = case["symbol"]
            pump_factor = case["pump_factor"]
            description = case["description"]
            notice_time = datetime.now() - timedelta(seconds=len(results)*10)
            
            print(f"\n📊 {symbol} 테스트 ({description})")
            print(f"공지시간: {notice_time.strftime('%H:%M:%S')}")
            print("1초간 데이터 생성 중...")
            
            # 데이터 생성
            start_time = time.time()
            trades, orderbook_snapshots = self.generate_ultra_fast_data(
                symbol, notice_time, pump_factor
            )
            
            # 분석
            analysis = self.analyze_ultra_fast_signal(trades, orderbook_snapshots)
            analysis_time = (time.time() - start_time) * 1000  # ms
            
            # 결과 출력
            print(f"⚡ 분석 소요시간: {analysis_time:.1f}ms")
            print(f"🎯 시그널 점수: {analysis['signal_score']}/100")
            print(f"📊 시그널 등급: {analysis['signal_grade']}")
            print(f"💡 권장 행동: {analysis['action']}")
            print(f"📈 최대 펌핑: {analysis['price_analysis']['max_pump_pct']:.2f}%")
            print(f"💰 매수 압력: {analysis['volume_analysis']['buy_pressure']:.1f}%")
            
            if analysis['signal_reasons']:
                print("🔍 시그널 이유:")
                for reason in analysis['signal_reasons']:
                    print(f"   - {reason}")
            
            # 시각화
            self.visualize_ultra_fast_data(symbol, notice_time, trades, orderbook_snapshots, analysis)
            
            results.append({
                'symbol': symbol,
                'analysis': analysis,
                'analysis_time_ms': analysis_time,
                'description': description
            })
        
        # 전체 요약
        print(f"\n" + "="*70)
        print("초고속 분석 요약")
        print("="*70)
        
        strong_signals = [r for r in results if r['analysis']['signal_score'] >= 60]
        avg_analysis_time = sum(r['analysis_time_ms'] for r in results) / len(results)
        
        print(f"평균 분석 속도: {avg_analysis_time:.1f}ms")
        print(f"강한 시그널 감지: {len(strong_signals)}/{len(results)}개")
        
        print(f"\n시그널 순위:")
        sorted_results = sorted(results, key=lambda x: x['analysis']['signal_score'], reverse=True)
        for i, result in enumerate(sorted_results):
            symbol = result['symbol']
            score = result['analysis']['signal_score']
            grade = result['analysis']['signal_grade']
            action = result['analysis']['action']
            print(f"  {i+1}. {symbol}: {score}점 - {grade} ({action})")
        
        return results

def main():
    """메인 실행"""
    analyzer = UltraFastPumpAnalyzer()
    analyzer.test_ultra_fast_analysis()

if __name__ == "__main__":
    main() 