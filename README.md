# Vocat Input 📚⚡

> **Vision OCR 기반 영어 단어장 자동 구조화 & 시험지 생성 시스템**

Vocat Input은 영어 단어장 교재 이미지에서 멀티 클라우드 Vision AI (AWS Bedrock, Google Vertex AI)를 활용하여 영어 단어, 한글 의미, 품사, 예문 및 **Dynamic Visual Bounding Box(BBox)**를 정밀하게 자동 추출하고, Vocat 앱 자동 입력용 JSON 및 시험지 DOC 문서로 변환하는 종합 자동화 시스템입니다.

---

## ✨ 주요 특징 (Key Features)

- 📷 **Multi-Cloud Vision OCR & AI Structuring**:
  - AWS Bedrock (`us.anthropic.claude-sonnet-4-6`, `claude-3-7-sonnet`) 및 Google Vertex AI (`gemini-2.5-pro`)를 통한 고성능 OCR 및 구조화.
- 🔍 **Interactive Evidence Bounding Box Viewer**:
  - 원본 단어 이미지 상에 근거 BBox 붉은 상자를 정확하게 렌더링.
  - 마우스 휠 줌(Zoom & Pan), 이전/다음 단어 연속 탐색 및 **키보드 화살표 단축키(`←` / `→`)** 지원.
- 💫 **Modern UI Design System**:
  - Raycast/Linear 감성의 `@property` conic-gradient 360도 회전 네온 테두리 빔(Rotational Border Beam) 및 2초 자동 반투명 스크롤바 적용.
- 📄 **Automated Document Generation**:
  - Vocat 앱 자동 입력 표준 JSON 규격 변환.
  - 단어 시험지 및 해답지 `.doc` 자동 생성.
- 📱 **Telegram & ADB Integration**:
  - 추출 완료 결과물 텔레그램 봇 자동 발송.
  - ADB UIAutomator를 통한 Vocat Android 앱 단어 자동 입력 스크립트 연동.

---

## 🛠️ 기술 스택 (Tech Stack)

### Backend & Engine
- **Language**: Go 1.25.7
- **CLI Framework**: Cobra, `flagsbinder`
- **Web Framework**: Gin Gonic (`gin-gonic/gin`), CORS
- **Cloud AI SDK**: AWS Bedrock REST API, Google Vertex AI Vision API

### Frontend Web App
- **Core Framework**: React 19, Vite
- **Styling**: TailwindCSS, Glassmorphism, CSS `@property` Conic Gradient Animations
- **UI Components & Icons**: Radix UI Dialog, Lucide React Icons

---

## 📁 프로젝트 구조 (Project Structure)

```
vocat-input/
├── cmd/
│   ├── vocat-server/     # REST API 및 Web UI 정적 서빙 백엔드 서버
│   ├── vocat-cli/        # CLI 엔드투엔드 단어 변환 및 자동화 도구
│   ├── test-clean/       # 변환 검증 샌드박스
│   └── roundtrip-test/   # AI BBox 및 OCR 파이프라인 정밀 테스트
├── internal/
│   └── engine/           # Vision OCR, AI Structuring, DOC/JSON 변환기, Telegram 모듈
├── web/                  # Vite + React 19 웹 프론트엔드 어플리케이션
├── storage/              # 업로드 이미지, 실행 데이터 및 생성 문서 저장소
├── docs/                 # 프로젝트 문서 및 변환 규격 가이드 모음
│   ├── doc-format.md
│   ├── instruction.md
│   ├── vocab_ocr_json_rules.md
│   ├── vocat-book.md
│   └── vocat-format.md
├── AGENTS.md             # AI 에이전트 개발 가이드
├── .env.template         # 환경 변수 템플릿
├── LICENSE               # MIT 라이선스
└── README.md             # 프로젝트 안내 문서 (본 문서)
```

---

## 🚀 시작하기 (Getting Started)

### 1. 환경 변수 설정

`.env.template` 파일을 참고하여 프로젝트 루트에 `.env` 파일을 생성합니다.

```bash
cp .env.template .env
```

`.env` 파일에 필요한 클라우드 API 키 및 세션 시크릿을 설정합니다:

```env
VOCAT_SESSION_SECRET=your-custom-session-secret
AWS_BEARER_TOKEN_BEDROCK=your-bedrock-token-here
# VERTEX_API_KEY=your-vertex-api-key-here
# TELEGRAM_BOT_TOKEN=your-telegram-bot-token
# TELEGRAM_CHAT_ID=your-telegram-chat-id
```

### 2. 백엔드 서버 빌드 및 실행

```bash
# 백엔드 서버 빌드
go build -o vocat-server ./cmd/vocat-server/

# 백엔드 서버 실행 (http://localhost:8080)
./vocat-server
```

### 3. 프론트엔드 빌드 (개발 시)

```bash
cd web
npm install
npm run build
```

### 4. CLI 도구 사용

단일 디렉터리 내 단어장 이미지를 한 번에 처리하여 JSON 및 DOC 파일로 출력합니다:

```bash
# CLI 빌드
go build -o vocat-cli ./cmd/vocat-cli/

# 실행 예시
./vocat-cli process --dir ./imgs/1 --out-json result.json --out-doc result.doc
```

---

## 📖 문서 가이드 (Documentation)

추가적인 개발 및 규격 문서는 `docs/` 디렉터리에서 확인하실 수 있습니다:

- [Vocat OCR JSON 규칙](docs/vocab_ocr_json_rules.md)
- [단어 시험지 DOC 생성 규격](docs/doc-format.md)
- [단어장 교재 파싱 구조 및 가이드](docs/vocat-book.md)
- [Vocat 앱 자동 입력 안내](docs/instruction.md)

---

## 📄 라이선스 (License)

이 프로젝트는 [MIT License](LICENSE)에 따라 자유롭게 이용하실 수 있습니다.
