#!/bin/bash

# 스크립트 로드
source ./vocat_input_v2.sh

echo "========== JSON 검증 테스트 =========="
echo ""

# 테스트 1: 유효한 JSON
echo "✓ 테스트 1: 유효한 JSON 파일"
if validate_json_file "words_test_small.json"; then
    echo "  → 통과"
else
    echo "  → 실패"
fi

echo ""

# 테스트 2: 없는 파일
echo "✓ 테스트 2: 없는 파일"
if validate_json_file "nonexistent.json" 2>/dev/null; then
    echo "  → 실패 (오류를 감지하지 못했음)"
else
    echo "  → 통과 (예상 오류 감지)"
fi

echo ""

# 테스트 3: 단어 항목 검증
echo "✓ 테스트 3: 단어 항목 검증"
word_json='{"no":1,"word":"test","pos":"noun","meaning":"테스트"}'
if validate_word_item "$word_json" 1; then
    echo "  → 통과"
else
    echo "  → 실패"
fi

echo ""

# 테스트 4: 품사 변환
echo "✓ 테스트 4: 품사 변환"
echo "  adjective → $(convert_pos 'adjective')"
echo "  noun → $(convert_pos 'noun')"
echo "  verb → $(convert_pos 'verb')"
echo "  adverb → $(convert_pos 'adverb')"

echo ""

# 테스트 5: 의미 분리
echo "✓ 테스트 5: 의미 분리"
echo "  입력: '맡기다, 배정하다'"
split_meanings "맡기다, 배정하다"

echo ""
echo "========== 테스트 완료 =========="
