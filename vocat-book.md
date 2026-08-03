# Vocat 단어장 파일 분석 (`aaa.doc`)

`aaa.doc`는 바이너리 문서가 아니라 **JSON 텍스트 파일**이며, Vocat 단어장 내보내기(또는 내부 저장) 포맷으로 보입니다.

## 1) 최상위 구조

최상위는 아래 2개 키로 구성됩니다.

```json
{
  "vocabulary": { ... },
  "corpusList": [ ... ]
}
```

- `vocabulary`: 단어장 메타데이터
- `corpusList`: 실제 단어(코퍼스) 배열

---

## 2) `vocabulary` 구조

확인된 필드:

- `id` (string): 단어장 ID
- `bookcaseId` (string): 상위 bookcase ID
- `name` (string): 단어장 이름
- `desc` (string): 설명
- `wordLang` (string): 단어 언어 (예: `englishUs`)
- `meaningLang` (string): 의미 언어 (예: `korean`)
- `total` (number): 전체 단어 수
- `nFamiliar` (number): 익숙한 단어 수
- `nUnfamiliar` (number): 익숙하지 않은 단어 수
- `price` (number)
- `isShowSchedule` (number, 0/1)
- `isSharable` (number, 0/1)
- `updatedAt` (string datetime, `...Z`)
- `createdAt` (string datetime, `...Z`)

예제:

```json
{
  "id": "11f1-1c79-d51acd00-9719-51a881aca281",
  "bookcaseId": "11f0-8ace-8b0804e0-801a-136621b43e19",
  "name": "20260312_Day1(p.12~p.27)",
  "desc": "",
  "wordLang": "englishUs",
  "meaningLang": "korean",
  "total": 80,
  "nFamiliar": 0,
  "nUnfamiliar": 0,
  "price": 0,
  "isShowSchedule": 1,
  "isSharable": 1,
  "updatedAt": "2026-03-10 12:22:38.531655Z",
  "createdAt": "2026-03-10 12:08:21.711978Z"
}
```

---

## 3) `corpusList` 구조

배열 길이: **80**

각 항목 필드:

- `id` (string): 단어 항목 ID
- `vocabularyId` (string): 상위 단어장 ID (`vocabulary.id`와 연결)
- `word` (string): 영어 단어/숙어
- `meaning` (string): 한국어 의미
- `pos` (string): 품사 축약 (예: `명`, `동`, `형`, `부`)
- `pronunciation` (null|string)
- `synonym` (null|string)
- `antonym` (null|string)
- `desc` (null|string)
- `image` (null|string)
- `familiar` (number, 보통 0/1)
- `scheduledAt` (string datetime, `...Z`)
- `updatedAt` (string datetime, `...Z`)
- `createdAt` (string datetime, `...Z`)

예제 1:

```json
{
  "id": "11f1-1c7a-f326a520-9719-51a881aca281",
  "vocabularyId": "11f1-1c79-d51acd00-9719-51a881aca281",
  "word": "acknowledge",
  "meaning": "인정하다",
  "pos": "동",
  "pronunciation": null,
  "synonym": null,
  "antonym": null,
  "desc": null,
  "image": null,
  "familiar": 0,
  "scheduledAt": "2026-03-10 12:16:21.618117Z",
  "updatedAt": "2026-03-10 12:16:21.618117Z",
  "createdAt": "2026-03-10 12:16:21.618117Z"
}
```

예제 2:

```json
{
  "id": "11f1-1c7a-fb257580-9719-51a881aca281",
  "vocabularyId": "11f1-1c79-d51acd00-9719-51a881aca281",
  "word": "alter",
  "meaning": "바꾸다",
  "pos": "동",
  "pronunciation": null,
  "synonym": null,
  "antonym": null,
  "desc": null,
  "image": null,
  "familiar": 0,
  "scheduledAt": "2026-03-10 12:16:35.032909Z",
  "updatedAt": "2026-03-10 12:16:35.032909Z",
  "createdAt": "2026-03-10 12:16:35.032909Z"
}
```

---

## 4) 관찰된 데이터 특성

- 품사 분포(aaa.doc 기준):
  - `동`: 28
  - `명`: 29
  - `형`: 22
  - `부`: 1

- 다중 의미는 문자열 내 구분자로 표현됨:
  - `﹒` 또는 `,` 포함 항목 존재
  - 예: `"기발한﹒독창적인"`

---

## 5) 변환 시 권장 매핑 (일반 단어 JSON → vocat 포맷)

- 입력 원본(예): `{ word, pos, meaning }`
- 출력 매핑:
  - `word` → `corpusList[].word`
  - `meaning`:
    - string[]이면 join
    - string에 쉼표가 있으면 분해 후 join 가능
    - 앱 호환을 위해 `﹒` 구분자 사용 가능
  - `pos`:
    - 영문 품사를 한글 축약으로 변환 권장
    - adjective→형, noun→명, verb→동, adverb→부, conjunction→접 ...

- ID:
  - 형식 예: `xxxx-xxxx-xxxxxxxx-xxxx-xxxxxxxxxxxx` (hex)
  - `vocabularyId`는 반드시 `vocabulary.id`와 동일 참조

- 시간:
  - `createdAt`, `updatedAt`, `scheduledAt`는 UTC `...Z` 문자열 사용

---

## 6) 최소 유효성 체크 포인트

- 최상위 키 `vocabulary`, `corpusList` 존재
- `corpusList`의 각 항목에 `id`, `vocabularyId`, `word`, `meaning`, `pos` 존재
- 모든 `corpusList[].vocabularyId == vocabulary.id`
- `vocabulary.total == len(corpusList)`

