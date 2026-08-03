# Vocat .doc 파일 포맷 명세

이 문서는 `vocat_convert.go`를 통해 생성되는 Vocat 단어장 `.doc` 파일의 내부 구조와 필드 정보를 설명합니다. 
`.doc` 확장자를 사용하지만 실제 데이터 형식은 **JSON**입니다.

---

## 1. 전체 구조

`.doc` 파일은 `vocabulary` 객체와 `corpusList` 배열로 구성된 JSON 객체입니다.

```json
{
  "vocabulary": {
    "id": "UUID",
    "bookcaseId": "UUID",
    "name": "단어장_이름",
    "desc": "",
    "wordLang": "englishUs",
    "meaningLang": "korean",
    "total": 80,
    "nFamiliar": 0,
    "nUnfamiliar": 0,
    "price": 0,
    "isShowSchedule": 1,
    "isSharable": 1,
    "updatedAt": "YYYY-MM-DD HH:MM:SS.ffffffZ",
    "createdAt": "YYYY-MM-DD HH:MM:SS.ffffffZ"
  },
  "corpusList": [
    {
      "id": "UUID",
      "vocabularyId": "UUID",
      "word": "단어",
      "meaning": "의미1﹒의미2",
      "pos": "품사",
      "pronunciation": null,
      "synonym": null,
      "antonym": null,
      "desc": null,
      "image": null,
      "familiar": 0,
      "scheduledAt": "YYYY-MM-DD HH:MM:SS.ffffffZ",
      "updatedAt": "YYYY-MM-DD HH:MM:SS.ffffffZ",
      "createdAt": "YYYY-MM-DD HH:MM:SS.ffffffZ"
    }
  ]
}
```

---

## 2. 주요 필드 설명

### 2.1 vocabulary (단어장 정보)
| 필드 | 설명 | 비고 |
|------|------|------|
| `id` | 단어장의 고유 ID | 랜덤 생성된 16진수 UUID 형식 |
| `bookcaseId` | 서재 ID | 랜덤 생성된 16진수 UUID 형식 |
| `name` | 단어장 제목 | 입력 파일명에서 확장자를 제외한 이름 |
| `total` | 단어 총 개수 | `corpusList`의 길이와 일치해야 함 |
| `updatedAt` | 업데이트 시간 | UTC 기준 `Z` 접미사 포함 |

### 2.2 corpusList (단어 목록)
| 필드 | 설명 | 비고 |
|------|------|------|
| `id` | 단어 항목의 고유 ID | 항목마다 개별 랜덤 생성 |
| `vocabularyId` | 소속 단어장 ID | `vocabulary.id`와 동일해야 함 (필수 연결) |
| `word` | 영어 단어 | 공백 불가 |
| `meaning` | 한국어 의미 | 다중 의미는 `﹒` (가운데 점)으로 구분 |
| `pos` | 품사 축약어 | 형, 명, 동, 부, 전, 접, 관, 감 등 한 글자 |
| `familiar` | 암기 여부 | 기본값 0 (미암기) |

---

## 3. 데이터 규칙

### 3.1 ID 형식
- 형식: `xxxx-xxxx-xxxxxxxx-xxxx-xxxxxxxxxxxx` (8-4-8-4-12 hex)
- 랜덤 16진수 바이트를 조합하여 생성합니다.

### 3.2 시간 형식 (Timestamp)
- 형식: `2006-01-02 15:04:05.000000Z`
- 반드시 UTC 시간이어야 하며, 끝에 `Z`가 붙어야 합니다.

### 3.3 의미(Meaning) 구분자
- 여러 개의 의미가 있을 경우 가운뎃점(`﹒`)을 사용합니다.
- 변환 시 괄호`()` 내부의 쉼표는 의미로 분리하지 않고 그대로 유지합니다.
- 예: `"(장소, 시간이) 일치하는, 부합하는"` → `"(장소, 시간이) 일치하는﹒부합하는"`

### 3.4 품사(POS) 축약
- `adjective` → `형`
- `noun` → `명`
- `verb` → `동`
- `adverb` → `부`
- 기타: 전, 접, 관, 감 등으로 매핑

---

## 4. 검증 요건 (Validation)

- **연결성**: 모든 `corpusList` 항목의 `vocabularyId`는 `vocabulary.id`와 완벽히 일치해야 합니다.
- **무결성**: `vocabulary.total` 값은 실제 `corpusList` 배열의 크기와 동일해야 합니다.
- **포맷**: 모든 타임스탬프 필드는 지정된 규격(`Z` 접미사)을 준수해야 합니다.
