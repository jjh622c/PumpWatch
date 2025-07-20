import pandas as pd
import json
from datetime import datetime, timedelta
import time
import os
from typing import List, Dict

# 각 모듈 import
from upbit_notice_collector import UpbitNoticeCollector
from exchange_trade_collector import TradeDataAnalyzer
from pump_visualizer import PumpVisualizer

class MainPumpAnalyzer:
    def __init__(self):
        self.notice_collector = UpbitNoticeCollector()
        self.trade_analyzer = TradeDataAnalyzer()
        self.visualizer = PumpVisualizer()
        
    def run_full_analysis(self, max_notices: int = 10, save_results: bool = True):
        """전체 분석 프로세스 실행"""
        
        print("="*60)
        print("업비트 상장 공지 펌핑 분석 시작")
        print("="*60)
        
        # 1단계: 업비트 상장 공지 수집
        print("\n1단계: 업비트 상장 공지 수집...")
        listing_notices = self.collect_listing_notices(max_notices)
        
        if not listing_notices:
            print("상장 공지를 찾을 수 없습니다.")
            return
        
        print(f"총 {len(listing_notices)}개의 상장 공지 발견")
        
        # 2단계: 각 상장 공지에 대해 체결 데이터 수집 및 분석
        analysis_results = []
        
        for i, notice in enumerate(listing_notices):
            print(f"\n2단계: {i+1}/{len(listing_notices)} - {notice['title']} 분석 중...")
            
            # 심볼이 있는 경우에만 분석
            if notice['symbols']:
                for symbol in notice['symbols']:
                    result = self.analyze_single_coin(notice, symbol)
                    if result:
                        analysis_results.append(result)
                        
                        # 시각화 및 저장
                        if save_results:
                            self.save_analysis_result(result)
                    
                    # API 제한 고려
                    time.sleep(1)
            else:
                print(f"  심볼을 찾을 수 없음: {notice['title']}")
        
        # 3단계: 전체 결과 요약
        print(f"\n3단계: 전체 분석 결과 요약...")
        self.create_summary_report(analysis_results, save_results)
        
        print("\n분석 완료!")
        return analysis_results
    
    def collect_listing_notices(self, max_notices: int) -> List[Dict]:
        """상장 공지 수집"""
        try:
            notices = self.notice_collector.collect_listing_notices(max_pages=10)
            
            # 최근 공지만 선택 (최대 max_notices개)
            if len(notices) > max_notices:
                notices = notices[:max_notices]
                
            return notices
        except Exception as e:
            print(f"공지 수집 오류: {e}")
            return []
    
    def analyze_single_coin(self, notice: Dict, symbol: str) -> Dict:
        """단일 코인 분석"""
        try:
            # 공지 시간 파싱
            notice_time = pd.to_datetime(notice['created_at'])
            
            print(f"  {symbol} 분석 중... (공지시간: {notice_time})")
            
            # 체결 데이터 수집 (공지 시점부터 1분간)
            pump_data = self.trade_analyzer.collect_pump_data(
                symbol=symbol, 
                notice_time=notice_time, 
                duration_minutes=1
            )
            
            # 데이터가 있는 경우에만 분석
            if pump_data['binance_trade_count'] > 0 or pump_data['bybit_trade_count'] > 0:
                
                # 상세 분석
                analysis_report = self.visualizer.create_detailed_analysis_report(pump_data)
                
                # 결과 통합
                result = {
                    'notice_info': notice,
                    'symbol': symbol,
                    'pump_data': pump_data,
                    'analysis': analysis_report,
                    'has_pump': self.detect_pump_signal(analysis_report)
                }
                
                # 결과 출력
                self.print_analysis_summary(result)
                
                return result
            else:
                print(f"    {symbol}: 체결 데이터 없음")
                return None
                
        except Exception as e:
            print(f"    {symbol} 분석 오류: {e}")
            return None
    
    def detect_pump_signal(self, analysis_report: Dict) -> Dict:
        """펌핑 시그널 감지"""
        pump_signals = {
            'binance_pump': False,
            'bybit_pump': False,
            'max_pump_pct': 0,
            'pump_detected': False
        }
        
        # 바이낸스 펌핑 확인
        binance_analysis = analysis_report.get('binance_analysis', {})
        if binance_analysis and 'max_pump_pct' in binance_analysis:
            binance_pump_pct = binance_analysis['max_pump_pct']
            if binance_pump_pct >= 10:  # 10% 이상 상승
                pump_signals['binance_pump'] = True
                pump_signals['max_pump_pct'] = max(pump_signals['max_pump_pct'], binance_pump_pct)
        
        # 바이비트 펌핑 확인
        bybit_analysis = analysis_report.get('bybit_analysis', {})
        if bybit_analysis and 'max_pump_pct' in bybit_analysis:
            bybit_pump_pct = bybit_analysis['max_pump_pct']
            if bybit_pump_pct >= 10:  # 10% 이상 상승
                pump_signals['bybit_pump'] = True
                pump_signals['max_pump_pct'] = max(pump_signals['max_pump_pct'], bybit_pump_pct)
        
        # 전체 펌핑 감지
        pump_signals['pump_detected'] = pump_signals['binance_pump'] or pump_signals['bybit_pump']
        
        return pump_signals
    
    def print_analysis_summary(self, result: Dict):
        """분석 결과 요약 출력"""
        symbol = result['symbol']
        pump_signals = result['has_pump']
        
        print(f"    {symbol} 분석 결과:")
        print(f"      바이낸스 체결: {result['pump_data']['binance_trade_count']}개")
        print(f"      바이비트 체결: {result['pump_data']['bybit_trade_count']}개")
        
        if pump_signals['pump_detected']:
            print(f"      🚀 펌핑 감지! 최대 상승률: {pump_signals['max_pump_pct']:.2f}%")
            if pump_signals['binance_pump']:
                print(f"         바이낸스 펌핑 확인")
            if pump_signals['bybit_pump']:
                print(f"         바이비트 펌핑 확인")
        else:
            print(f"      😴 펌핑 없음 (최대 상승률: {pump_signals['max_pump_pct']:.2f}%)")
    
    def save_analysis_result(self, result: Dict):
        """분석 결과 저장"""
        symbol = result['symbol']
        notice_time = result['pump_data']['notice_time']
        time_str = notice_time.strftime('%Y%m%d_%H%M%S')
        
        # 디렉토리 생성
        os.makedirs('analysis_results', exist_ok=True)
        
        # 1. JSON 리포트 저장
        json_filename = f"analysis_results/{symbol}_{time_str}_report.json"
        with open(json_filename, 'w', encoding='utf-8') as f:
            # datetime 객체를 문자열로 변환하여 저장
            json_data = self.convert_datetime_to_string(result)
            json.dump(json_data, f, ensure_ascii=False, indent=2)
        
        # 2. 시각화 저장
        if result['pump_data']['binance_trade_count'] > 0 or result['pump_data']['bybit_trade_count'] > 0:
            chart_filename = f"analysis_results/{symbol}_{time_str}_chart.png"
            self.visualizer.visualize_exchange_comparison(result['pump_data'], chart_filename)
        
        # 3. CSV 데이터 저장
        self.trade_analyzer.save_trades_to_csv(result['pump_data'], f"analysis_results/{symbol}_{time_str}")
        
        print(f"    결과 저장 완료: {symbol}_{time_str}")
    
    def convert_datetime_to_string(self, obj):
        """JSON 직렬화를 위해 datetime 객체를 문자열로 변환"""
        if isinstance(obj, dict):
            return {key: self.convert_datetime_to_string(value) for key, value in obj.items()}
        elif isinstance(obj, list):
            return [self.convert_datetime_to_string(item) for item in obj]
        elif isinstance(obj, datetime):
            return obj.isoformat()
        else:
            return obj
    
    def create_summary_report(self, analysis_results: List[Dict], save_results: bool = True):
        """전체 분석 결과 요약 리포트 생성"""
        if not analysis_results:
            print("분석 결과가 없습니다.")
            return
        
        # 통계 계산
        total_analyzed = len(analysis_results)
        pumped_count = sum(1 for result in analysis_results if result['has_pump']['pump_detected'])
        pump_rate = (pumped_count / total_analyzed) * 100 if total_analyzed > 0 else 0
        
        # 거래소별 펌핑 통계
        binance_pumps = sum(1 for result in analysis_results if result['has_pump']['binance_pump'])
        bybit_pumps = sum(1 for result in analysis_results if result['has_pump']['bybit_pump'])
        
        # 평균 펌핑률 계산
        pump_percentages = [result['has_pump']['max_pump_pct'] for result in analysis_results 
                          if result['has_pump']['pump_detected']]
        avg_pump = sum(pump_percentages) / len(pump_percentages) if pump_percentages else 0
        max_pump = max(pump_percentages) if pump_percentages else 0
        
        # 요약 출력
        print("\n" + "="*60)
        print("전체 분석 결과 요약")
        print("="*60)
        print(f"총 분석 코인 수: {total_analyzed}")
        print(f"펌핑 발생 코인 수: {pumped_count}")
        print(f"펌핑 발생률: {pump_rate:.1f}%")
        print(f"바이낸스 펌핑: {binance_pumps}개")
        print(f"바이비트 펌핑: {bybit_pumps}개")
        print(f"평균 펌핑률: {avg_pump:.2f}%")
        print(f"최대 펌핑률: {max_pump:.2f}%")
        
        # 상위 펌핑 코인들
        print(f"\n상위 펌핑 코인들:")
        pumped_results = [result for result in analysis_results if result['has_pump']['pump_detected']]
        pumped_results.sort(key=lambda x: x['has_pump']['max_pump_pct'], reverse=True)
        
        for i, result in enumerate(pumped_results[:5]):
            symbol = result['symbol']
            pump_pct = result['has_pump']['max_pump_pct']
            notice_time = result['pump_data']['notice_time']
            print(f"  {i+1}. {symbol}: {pump_pct:.2f}% ({notice_time.strftime('%Y-%m-%d %H:%M')})")
        
        # 요약 저장
        if save_results:
            summary_data = {
                'analysis_date': datetime.now().isoformat(),
                'statistics': {
                    'total_analyzed': total_analyzed,
                    'pumped_count': pumped_count,
                    'pump_rate': pump_rate,
                    'binance_pumps': binance_pumps,
                    'bybit_pumps': bybit_pumps,
                    'avg_pump': avg_pump,
                    'max_pump': max_pump
                },
                'top_pumps': [
                    {
                        'symbol': result['symbol'],
                        'pump_pct': result['has_pump']['max_pump_pct'],
                        'notice_time': result['pump_data']['notice_time'].isoformat(),
                        'title': result['notice_info']['title']
                    }
                    for result in pumped_results[:10]
                ]
            }
            
            os.makedirs('analysis_results', exist_ok=True)
            summary_filename = f"analysis_results/summary_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"
            
            with open(summary_filename, 'w', encoding='utf-8') as f:
                json.dump(summary_data, f, ensure_ascii=False, indent=2)
            
            print(f"\n요약 리포트 저장: {summary_filename}")

def main():
    """메인 실행 함수"""
    analyzer = MainPumpAnalyzer()
    
    print("업비트 상장 공지 펌핑 분석기")
    print("분석할 최근 공지 수를 입력하세요 (기본값: 5): ", end="")
    
    try:
        user_input = input().strip()
        max_notices = int(user_input) if user_input else 5
    except ValueError:
        max_notices = 5
    
    print(f"\n최근 {max_notices}개의 상장 공지를 분석합니다...")
    
    # 전체 분석 실행
    results = analyzer.run_full_analysis(max_notices=max_notices, save_results=True)
    
    print("\n분석이 완료되었습니다!")
    print("analysis_results/ 디렉토리에서 결과를 확인하세요.")

if __name__ == "__main__":
    main() 