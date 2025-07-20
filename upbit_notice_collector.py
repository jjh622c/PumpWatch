import requests
import json
import pandas as pd
from datetime import datetime
import time
import re

class UpbitNoticeCollector:
    def __init__(self):
        self.base_url = "https://api-manager.upbit.com/api/v1/notices"
        self.headers = {
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
        }
    
    def get_notices(self, page=1, per_page=20):
        """업비트 공지사항 목록 조회"""
        params = {
            'page': page,
            'per_page': per_page,
            'thread_name': 'general'  # 일반 공지사항
        }
        
        try:
            response = requests.get(self.base_url, headers=self.headers, params=params)
            response.raise_for_status()
            return response.json()
        except Exception as e:
            print(f"공지사항 조회 오류: {e}")
            return None
    
    def get_notice_detail(self, notice_id):
        """특정 공지사항 상세 조회"""
        url = f"{self.base_url}/{notice_id}"
        
        try:
            response = requests.get(url, headers=self.headers)
            response.raise_for_status()
            return response.json()
        except Exception as e:
            print(f"공지사항 상세 조회 오류: {e}")
            return None
    
    def is_listing_notice(self, title, content=""):
        """상장 공지인지 판단"""
        listing_keywords = [
            '상장', '신규거래', '신규 거래', '거래지원', '거래 지원',
            'NEW LISTING', 'listing', 'LISTING'
        ]
        
        title_lower = title.lower()
        content_lower = content.lower()
        
        for keyword in listing_keywords:
            if keyword.lower() in title_lower or keyword.lower() in content_lower:
                return True
        return False
    
    def extract_coin_symbols(self, title, content=""):
        """공지에서 코인 심볼 추출"""
        # 일반적인 코인 심볼 패턴 (3-10자의 대문자)
        symbol_pattern = r'\b([A-Z]{2,10})\b'
        
        # 제목과 내용에서 심볼 추출
        title_symbols = re.findall(symbol_pattern, title)
        content_symbols = re.findall(symbol_pattern, content) if content else []
        
        # 중복 제거 및 일반적이지 않은 단어 필터링
        exclude_words = {'KRW', 'BTC', 'USDT', 'NEW', 'COIN', 'LISTING', 'UPBIT'}
        symbols = list(set(title_symbols + content_symbols) - exclude_words)
        
        return symbols
    
    def collect_listing_notices(self, max_pages=10):
        """상장 공지 수집"""
        listing_notices = []
        
        for page in range(1, max_pages + 1):
            print(f"페이지 {page} 수집 중...")
            
            notices_data = self.get_notices(page=page, per_page=20)
            if not notices_data or 'data' not in notices_data:
                break
            
            notices = notices_data['data']['list']
            if not notices:
                break
            
            for notice in notices:
                title = notice.get('title', '')
                
                # 상장 공지인지 확인
                if self.is_listing_notice(title):
                    # 상세 정보 조회
                    detail = self.get_notice_detail(notice['id'])
                    
                    if detail and 'data' in detail:
                        notice_detail = detail['data']
                        content = notice_detail.get('body', '')
                        
                        # 코인 심볼 추출
                        symbols = self.extract_coin_symbols(title, content)
                        
                        listing_info = {
                            'id': notice['id'],
                            'title': title,
                            'created_at': notice['created_at'],
                            'updated_at': notice['updated_at'],
                            'symbols': symbols,
                            'content': content[:500]  # 내용 일부만 저장
                        }
                        
                        listing_notices.append(listing_info)
                        print(f"상장 공지 발견: {title} - {symbols}")
                    
                    # API 호출 제한 고려
                    time.sleep(0.5)
            
            # 페이지 간 대기
            time.sleep(1)
        
        return listing_notices
    
    def save_to_csv(self, notices, filename="upbit_listing_notices.csv"):
        """CSV 파일로 저장"""
        if not notices:
            print("저장할 공지사항이 없습니다.")
            return
        
        # DataFrame 생성
        df = pd.DataFrame(notices)
        
        # 날짜 형식 변환
        df['created_at'] = pd.to_datetime(df['created_at'])
        df['updated_at'] = pd.to_datetime(df['updated_at'])
        
        # symbols 리스트를 문자열로 변환
        df['symbols_str'] = df['symbols'].apply(lambda x: ','.join(x) if x else '')
        
        # CSV 저장
        df.to_csv(filename, index=False, encoding='utf-8-sig')
        print(f"파일 저장 완료: {filename}")
        print(f"총 {len(df)}개의 상장 공지 수집")
        
        return df

def main():
    """메인 실행 함수"""
    collector = UpbitNoticeCollector()
    
    print("업비트 상장 공지 수집 시작...")
    notices = collector.collect_listing_notices(max_pages=20)
    
    if notices:
        df = collector.save_to_csv(notices)
        
        # 결과 요약 출력
        print("\n=== 수집 결과 요약 ===")
        print(f"총 상장 공지 수: {len(notices)}")
        
        # 최근 5개 공지 출력
        print("\n최근 5개 상장 공지:")
        for i, notice in enumerate(notices[:5]):
            print(f"{i+1}. {notice['created_at'][:19]} - {notice['title']}")
            print(f"   코인: {notice['symbols']}")
    else:
        print("수집된 상장 공지가 없습니다.")

if __name__ == "__main__":
    main() 