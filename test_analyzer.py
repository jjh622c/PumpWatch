"""
업비트 상장 공지 펌핑 분석 테스트 버전
실제 API 호출 없이 샘플 데이터로 시연
"""

import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
from datetime import datetime, timedelta
import json
import os

# 한글 폰트 설정
plt.rcParams['font.family'] = 'Malgun Gothic'
plt.rcParams['axes.unicode_minus'] = False

def generate_sample_data():
    """샘플 상장 공지 및 체결 데이터 생성"""
    
    # 샘플 상장 공지
    sample_notices = [
        {
            'id': 1,
            'title': '[거래지원] Swell (SWELL) 원화 마켓 거래 지원 안내',
            'created_at': '2024-01-15T14:30:00',
            'symbols': ['SWELL'],
            'content': 'Swell 코인의 원화 마켓 거래를 지원합니다.'
        },
        {
            'id': 2,
            'title': '[거래지원] Cartesi (CTSI) 원화 마켓 거래 지원 안내',
            'created_at': '2024-01-10T09:00:00',
            'symbols': ['CTSI'],
            'content': 'Cartesi 코인의 원화 마켓 거래를 지원합니다.'
        },
        {
            'id': 3,
            'title': '[신규상장] Pepe (PEPE) 디지털 자산 추가',
            'created_at': '2024-01-05T16:45:00',
            'symbols': ['PEPE'],
            'content': 'Pepe 코인을 신규 상장합니다.'
        }
    ]
    
    return sample_notices

def generate_trading_data(symbol, notice_time, pump_factor=1.0):
    """샘플 체결 데이터 생성"""
    
    base_price = 100.0  # 기준 가격
    base_volume = 10.0  # 기준 거래량
    
    # 1분간의 데이터 (60초)
    trades = []
    current_time = notice_time
    current_price = base_price
    
    for i in range(60):  # 60초간
        # 펌핑 효과 시뮬레이션
        if i < 30:  # 처음 30초간 펌핑
            price_change = np.random.normal(0.5 * pump_factor, 0.2)
            volume_multiplier = 2 + pump_factor
        else:  # 나중 30초간 안정화
            price_change = np.random.normal(-0.1, 0.1)
            volume_multiplier = 1.0
        
        current_price += price_change
        volume = base_volume * volume_multiplier * np.random.uniform(0.5, 1.5)
        
        trade = {
            'exchange': 'binance',
            'symbol': symbol,
            'price': max(current_price, 0.1),  # 최소 가격 보장
            'quantity': max(volume, 0.1),  # 최소 거래량 보장
            'timestamp': current_time + timedelta(seconds=i),
            'is_buyer_maker': np.random.choice([True, False])
        }
        trades.append(trade)
    
    return trades

def analyze_pump_pattern(trades_data):
    """펌핑 패턴 분석"""
    if not trades_data:
        return {}
    
    df = pd.DataFrame(trades_data)
    df = df.sort_values('timestamp')
    
    analysis = {
        'total_trades': len(df),
        'total_volume': df['quantity'].sum(),
        'price_first': df['price'].iloc[0],
        'price_last': df['price'].iloc[-1],
        'price_max': df['price'].max(),
        'price_min': df['price'].min()
    }
    
    # 가격 변동률 계산
    if analysis['price_first'] > 0:
        analysis['price_change_pct'] = ((analysis['price_last'] - analysis['price_first']) / analysis['price_first']) * 100
        analysis['max_pump_pct'] = ((analysis['price_max'] - analysis['price_first']) / analysis['price_first']) * 100
    else:
        analysis['price_change_pct'] = 0
        analysis['max_pump_pct'] = 0
    
    return analysis

def visualize_pump_data(symbol, notice_time, binance_trades, bybit_trades):
    """펌핑 데이터 시각화"""
    
    fig, axes = plt.subplots(2, 2, figsize=(15, 10))
    fig.suptitle(f'{symbol} 상장 공지 펌핑 분석 - {notice_time.strftime("%Y-%m-%d %H:%M:%S")}', 
                 fontsize=16, fontweight='bold')
    
    # 1. 가격 변동 (좌상단)
    ax1 = axes[0, 0]
    if binance_trades:
        binance_df = pd.DataFrame(binance_trades)
        ax1.plot(binance_df['timestamp'], binance_df['price'], 'b-', label='바이낸스', linewidth=2)
    
    if bybit_trades:
        bybit_df = pd.DataFrame(bybit_trades)
        ax1.plot(bybit_df['timestamp'], bybit_df['price'], 'r-', label='바이비트', linewidth=2)
    
    ax1.axvline(x=notice_time, color='orange', linestyle='--', label='상장 공지')
    ax1.set_title('거래소별 가격 변동')
    ax1.set_xlabel('시간')
    ax1.set_ylabel('가격 (USDT)')
    ax1.legend()
    ax1.grid(True, alpha=0.3)
    
    # 2. 거래량 (우상단)
    ax2 = axes[0, 1]
    if binance_trades:
        binance_df = pd.DataFrame(binance_trades)
        binance_df['time_bucket'] = binance_df['timestamp'].dt.floor('10S')
        binance_volume = binance_df.groupby('time_bucket')['quantity'].sum()
        ax2.bar(binance_volume.index, binance_volume.values, alpha=0.7, label='바이낸스', color='blue', width=timedelta(seconds=8))
    
    if bybit_trades:
        bybit_df = pd.DataFrame(bybit_trades)
        bybit_df['time_bucket'] = bybit_df['timestamp'].dt.floor('10S')
        bybit_volume = bybit_df.groupby('time_bucket')['quantity'].sum()
        ax2.bar(bybit_volume.index, bybit_volume.values, alpha=0.7, label='바이비트', color='red', width=timedelta(seconds=8))
    
    ax2.axvline(x=notice_time, color='orange', linestyle='--', label='상장 공지')
    ax2.set_title('거래소별 체결량 (10초 간격)')
    ax2.set_xlabel('시간')
    ax2.set_ylabel('체결량')
    ax2.legend()
    ax2.grid(True, alpha=0.3)
    
    # 3. 체결 빈도 (좌하단)
    ax3 = axes[1, 0]
    if binance_trades:
        binance_df = pd.DataFrame(binance_trades)
        binance_df['time_bucket'] = binance_df['timestamp'].dt.floor('5S')
        binance_freq = binance_df.groupby('time_bucket').size()
        ax3.plot(binance_freq.index, binance_freq.values, 'b-o', label='바이낸스', markersize=4)
    
    if bybit_trades:
        bybit_df = pd.DataFrame(bybit_trades)
        bybit_df['time_bucket'] = bybit_df['timestamp'].dt.floor('5S')
        bybit_freq = bybit_df.groupby('time_bucket').size()
        ax3.plot(bybit_freq.index, bybit_freq.values, 'r-s', label='바이비트', markersize=4)
    
    ax3.axvline(x=notice_time, color='orange', linestyle='--', label='상장 공지')
    ax3.set_title('시간별 체결 빈도 (5초 간격)')
    ax3.set_xlabel('시간')
    ax3.set_ylabel('체결 횟수')
    ax3.legend()
    ax3.grid(True, alpha=0.3)
    
    # 4. 가격-거래량 산점도 (우하단)
    ax4 = axes[1, 1]
    if binance_trades:
        binance_df = pd.DataFrame(binance_trades)
        ax4.scatter(binance_df['price'], binance_df['quantity'], alpha=0.6, label='바이낸스', color='blue', s=20)
    
    if bybit_trades:
        bybit_df = pd.DataFrame(bybit_trades)
        ax4.scatter(bybit_df['price'], bybit_df['quantity'], alpha=0.6, label='바이비트', color='red', s=20)
    
    ax4.set_title('가격 vs 체결량')
    ax4.set_xlabel('가격 (USDT)')
    ax4.set_ylabel('체결량')
    ax4.legend()
    ax4.grid(True, alpha=0.3)
    
    plt.tight_layout()
    
    # 저장
    os.makedirs('analysis_results', exist_ok=True)
    filename = f"analysis_results/{symbol}_{notice_time.strftime('%Y%m%d_%H%M%S')}_analysis.png"
    plt.savefig(filename, dpi=300, bbox_inches='tight')
    print(f"차트 저장: {filename}")
    
    plt.show()

def main():
    """메인 실행 함수"""
    print("="*60)
    print("업비트 상장 공지 펌핑 분석 시스템 (테스트 버전)")
    print("="*60)
    
    # 샘플 데이터 생성
    sample_notices = generate_sample_data()
    
    print(f"\n샘플 상장 공지 {len(sample_notices)}개를 분석합니다...\n")
    
    results = []
    
    for i, notice in enumerate(sample_notices):
        symbol = notice['symbols'][0]
        notice_time = pd.to_datetime(notice['created_at'])
        
        print(f"{i+1}. {notice['title']}")
        print(f"   심볼: {symbol}")
        print(f"   공지시간: {notice_time}")
        
        # 펌핑 강도를 다르게 설정 (첫 번째는 강한 펌핑, 나머지는 약한 펌핑)
        pump_factor = 3.0 if i == 0 else np.random.uniform(0.5, 1.5)
        
        # 샘플 체결 데이터 생성
        binance_trades = generate_trading_data(symbol, notice_time, pump_factor)
        bybit_trades = generate_trading_data(symbol, notice_time, pump_factor * 0.8)  # 바이비트는 약간 낮게
        
        # 분석
        binance_analysis = analyze_pump_pattern(binance_trades)
        bybit_analysis = analyze_pump_pattern(bybit_trades)
        
        # 펌핑 감지
        max_pump = max(binance_analysis.get('max_pump_pct', 0), bybit_analysis.get('max_pump_pct', 0))
        is_pump = max_pump >= 10.0
        
        print(f"   바이낸스: {len(binance_trades)}개 체결, 최대 펌핑: {binance_analysis.get('max_pump_pct', 0):.2f}%")
        print(f"   바이비트: {len(bybit_trades)}개 체결, 최대 펌핑: {bybit_analysis.get('max_pump_pct', 0):.2f}%")
        
        if is_pump:
            print(f"   🚀 펌핑 감지! 최대 상승률: {max_pump:.2f}%")
        else:
            print(f"   😴 펌핑 없음 (최대 상승률: {max_pump:.2f}%)")
        
        # 시각화
        visualize_pump_data(symbol, notice_time, binance_trades, bybit_trades)
        
        # 결과 저장
        result = {
            'symbol': symbol,
            'notice_time': notice_time.isoformat(),
            'title': notice['title'],
            'binance_analysis': binance_analysis,
            'bybit_analysis': bybit_analysis,
            'max_pump_pct': max_pump,
            'is_pump': is_pump
        }
        results.append(result)
        
        print()
    
    # 전체 요약
    print("\n" + "="*60)
    print("전체 분석 결과 요약")
    print("="*60)
    
    total_analyzed = len(results)
    pumped_count = sum(1 for r in results if r['is_pump'])
    pump_rate = (pumped_count / total_analyzed) * 100 if total_analyzed > 0 else 0
    
    print(f"총 분석 코인 수: {total_analyzed}")
    print(f"펌핑 발생 코인 수: {pumped_count}")
    print(f"펌핑 발생률: {pump_rate:.1f}%")
    
    # 상위 펌핑 코인
    results.sort(key=lambda x: x['max_pump_pct'], reverse=True)
    print(f"\n상위 펌핑 코인:")
    for i, result in enumerate(results[:3]):
        print(f"  {i+1}. {result['symbol']}: {result['max_pump_pct']:.2f}%")
    
    # 결과 JSON 저장
    os.makedirs('analysis_results', exist_ok=True)
    summary_file = f"analysis_results/summary_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"
    
    with open(summary_file, 'w', encoding='utf-8') as f:
        json.dump({
            'analysis_date': datetime.now().isoformat(),
            'total_analyzed': total_analyzed,
            'pumped_count': pumped_count,
            'pump_rate': pump_rate,
            'results': results
        }, f, ensure_ascii=False, indent=2)
    
    print(f"\n결과 저장: {summary_file}")
    print(f"차트 저장: analysis_results/ 디렉토리")
    print("\n테스트 완료! 🎉")

if __name__ == "__main__":
    main() 