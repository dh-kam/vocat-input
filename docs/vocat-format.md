# Vocat 파일 형식 정의(vocat-format.md)

이 문서는 `vocat-auto` 프로젝트에서 사용되는 Vocat 단어장 관련 파일 형식을 정리한 기준 문서입니다.

## 1. 파일 구분

Vocat 파이프라인에서 실제로 다루는 파일 형식은 크게 2종입니다.

1. `단어 입력 원본 JSON` (OCR/수동 작성 입력)
2. `Vocat 단어장 파일` (`.doc` 확장자이지만 JSON 텍스트)

---

## 2. 단어 입력 원본 JSON 형식

### 2.1 기본 형태

```json
[
  {
    "no": 1,
    "word": "critical",
    "pos": "adjective",
    "meaning": "위독한"
  },
  {
    "no": 2,
    "word": "assign",
    "pos": "verb",
    "meaning": "맡기다, 배정하다"
  }
]
```

### 2.2 필드 규칙

- `no`: number (필수)
  - 원본 번호 정보를 그대로 유지하는 용도
- `word`: string (필수, 빈 문자열 불가)
- `pos`: string (필수)
  - 영문 POS(`adjective|noun|verb|adverb|preposition|conjunction|article|interjection`) 또는 한국어 POS 사용 가능
- `meaning`: string 또는 string[] (필수)
  - 문자열인 경우 쉼표(`,`)로 다중 의미 표기 가능
  - 배열인 경우 항목 각각이 의미 조각

### 2.3 의미 분리 규칙(입력 파싱 기준)

- 쉼표 구분은 기본적으로 의미 분리 대상으로 간주하되,
  괄호 안의 쉼표는 분리하면 안 됩니다.
- 예시: `"(장소, 시간이) 일치하는"`는 하나의 의미로 처리

### 2.4 POS 매핑 규칙(원본 → 축약)

- `adjective` → `형`
- `noun` → `명`
- `verb` → `동`
- `adverb` → `부`
- `preposition` → `전`
- `conjunction` → `접`
- `article` → `관`
- `interjection` → `감`
- 한국어 POS(`형|명|동|부|전|접|관|감`)는 그대로 통과

---

## 3. Vocat 단어장 파일(`.doc`) 형식

`vocat_convert.go`가 출력하는 실제 포맷은 최상위 JSON 객체입니다.

```json
{
  "vocabulary": { ... },
  "corpusList": [ ... ]
}
```

### 3.1 `vocabulary` 객체

필수/권장 키

- `id` (string): vocabulary 고유 ID
- `bookcaseId` (string)
- `name` (string): 단어장 이름
- `desc` (string)
- `wordLang` (string): 예 `englishUs`
- `meaningLang` (string): 예 `korean`
- `total` (number): 단어 수
- `nFamiliar` (number)
- `nUnfamiliar` (number)
- `price` (number)
- `isShowSchedule` (number, 보통 1)
- `isSharable` (number, 보통 1)
- `updatedAt` (string, UTC 타임스탬프, `...Z`)
- `createdAt` (string, UTC 타임스탬프, `...Z`)

### 3.2 `corpusList` 항목

각 항목은 단어 엔트리입니다.

- `id` (string): 항목 고유 ID
- `vocabularyId` (string): `vocabulary.id`와 동일해야 함
- `word` (string): 영단어
- `meaning` (string): 한글 의미(다중 의미는 문자열)
- `pos` (string): 한글 축약 POS (`명`, `동`, `형`, `부`, `전`, `접`, `관`, `감`)
- `pronunciation` (nullable)
- `synonym` (nullable)
- `antonym` (nullable)
- `desc` (nullable)
- `image` (nullable)
- `familiar` (number): 보통 0
- `scheduledAt` (string, UTC 타임스탬프)
- `updatedAt` (string, UTC 타임스탬프)
- `createdAt` (string, UTC 타임스탬프)

### 3.3 다중 의미 표현

- 변환기 출력 기본은 의미 조각을 `,`가 아닌 `﹒`(U+FE52)로 join
  - 예: `"그저﹒단순히"`
- 이 값은 앱 입력에서 한 덩어리 문자열로 처리

### 3.4 ID 형식

코드에서 생성되는 ID 규칙은 아래와 같은 하이픈 그룹 문자열 형식입니다.

`XXXXXXXX-XXXX-XXXXXXXX-XXXX-XXXXXXXXXXXX`

(`vocat_convert.go`의 `randomID`와 `vocat_book` 검증 코드에서 기대)

### 3.5 시간 형식

`YYYY-MM-DD HH:MM:SS.ffffffZ` (예: `2026-03-10 12:22:38.531655Z`)

---

## 4. 검증 규칙 정리

### 4.1 입력 원본 JSON 검증

- 파일 존재
- JSON 파싱 가능
- 배열 형태
- `no`, `word`, `pos`, `meaning` 존재
- `word` / `meaning` 비어있으면 실패

### 4.2 변환 결과 검증

- `vocabulary` 및 `corpusList` 존재
- `vocabulary.total == len(corpusList)`
- `vocabulary.id`와 각 `corpusList[i].vocabularyId` 일치
- `updatedAt`, `createdAt`, `scheduledAt` 타입 문자열, 끝이 `Z`

### 4.3 입력용 `vocat_validate.go` 기대 사항(기존 Vocat Export 파일)

- 최상위 구조가 위의 두 키를 가져야 함
- `vocabulary.total` 정합성(단어 수)
- 각 `corpusList` 항목의 필수 필드 존재
- 각 항목 `meaning` 비어있으면 실패

---

## 5. 사용 예시

### 5.1 원본 JSON 예시

```json
{
  "no": 1,
  "word": "bitter",
  "pos": "adjective",
  "meaning": ["혹독한", "매서운"]
}
```

### 5.2 출력 `.doc` 예시

```json
{
  "vocabulary": {
    "id": "11f1-1c79-d51acd00-9719-51a881aca281",
    "bookcaseId": "11f0-8ace-8b0804e0-801a-136621b43e19",
    "name": "example",
    "desc": "",
    "wordLang": "englishUs",
    "meaningLang": "korean",
    "total": 1,
    "nFamiliar": 0,
    "nUnfamiliar": 0,
    "price": 0,
    "isShowSchedule": 1,
    "isSharable": 1,
    "updatedAt": "2026-06-07 00:00:00.000000Z",
    "createdAt": "2026-06-07 00:00:00.000000Z"
  },
  "corpusList": [
    {
      "id": "abcd-ef12-1234abcd-5678-123456abcdef",
      "vocabularyId": "11f1-1c79-d51acd00-9719-51a881aca281",
      "word": "bitter",
      "meaning": "혹독한﹒매서운",
      "pos": "형",
      "pronunciation": null,
      "synonym": null,
      "antonym": null,
      "desc": null,
      "image": null,
      "familiar": 0,
      "scheduledAt": "2026-06-07 00:00:00.000000Z",
      "updatedAt": "2026-06-07 00:00:00.000000Z",
      "createdAt": "2026-06-07 00:00:00.000000Z"
    }
  ]
}
```

---

## 6. 자주 실수하는 포인트

- `.doc` 확장자라고 해서 일반 텍스트(.txt)와 다르다고 착각하지 말고, JSON 텍스트임을 인식
- `meaning`의 다중 값은 실제로 문자열 하나로 저장됨
- `vocabularyId` 불일치 시 Vocat 앱/검증 단계에서 오류 발생
- 타임스탬프는 `Z` 접미사를 필수로 유지
- `vocat_input.go`는 현재 단일 단어 입력 테스트 용도로 동작하므로, 다중 단어 자동 입력은 `vocat_input.sh`를 기준으로 확인
