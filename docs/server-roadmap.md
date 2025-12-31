# Server Roadmap (v3) — Web Terminal / PTY

> 이 문서는 v3 서버 구현을 **5개 PR(STASK-1~5)** 로 쪼개기 위한 로드맵이다.  
> 각 STASK는 Git PR tag로 사용된다.

---

## 0. PR 컨벤션

- PR 제목 예시: `[STASK-1] PTY session foundation (create/terminate + cwd workspace boundary)`
- 원칙
  - 각 PR은 “논리적으로 완결”되어야 한다(단독으로 리뷰/테스트 가능).
  - 다음 STASK가 진행될 수 있을 만큼 계약(인터페이스/설정/테스트)을 고정한다.

---

## 1. v3 서버 산출물 요약(최종 상태)

### 1.1 필수 엔드포인트
- `POST /api/pty/sessions`
- `GET (WebSocket) /api/pty/sessions/{id}/ws`
- `DELETE /api/pty/sessions/{id}`

### 1.2 핵심 정책
- **workspace 경계 강제** (workspace → local_path 기반 cwd 해석)
- **disconnect 정리 정책 일관**: `terminate_immediately`(권장) 또는 `idle_timeout`
- **좀비/FD 누수 없이 종료** (kill + wait + close)
- 보안 최소: command blacklist(가드레일), auth scaffolding(default off)

---

## 2. STASK 개요

| STASK | 목표(한 줄) | 선행 | 핵심 테스트 |
|---|---|---|---|
| STASK-1 | 세션 리소스 모델 + HTTP create/terminate + cwd workspace boundary | - | create/terminate + wait 검증 |
| STASK-2 | WS attach + 입출력 스트리밍 + resize + single-attach | STASK-1 | WS echo roundtrip |
| STASK-3 | 라이프사이클 하드닝(정리 정책 고정/exit 처리/limits/logging) | STASK-2 | disconnect 종료 정책 검증 |
| STASK-4 | command blacklist(서버 가드레일) | STASK-2 | blocked 패턴 차단 검증 |
| STASK-5 | 옵션 토큰 인증 스캐폴딩(default off) + 폴리시 마감 | STASK-2 | auth on/off 회귀 테스트 |

---

## 3. STASK 상세

## STASK-1 — PTY Foundation (SessionManager + HTTP create/terminate + CWD workspace boundary)

### 목적
- 서버에 “PTY 세션”이라는 리소스를 도입한다.
- **세션 생성/종료가 정확히 동작**하고, **cwd가 workspace 경계 안에서만** 허용된다.
- 이후 WS attach(STASK-2)가 얹힐 수 있는 내부 구조(세션 매니저/세션 구조체/에러 코드)를 고정한다.

### 포함 범위
- in-memory session manager
- PTY spawn (`creack/pty`)
- cwd 해석: `workspace_id + cwd_rel → resolved_cwd`
- workspace 경계 검증(필수)
- HTTP:
  - `POST /api/pty/sessions`
  - `DELETE /api/pty/sessions/{id}`

### 구현 가이드
1) **모듈 경계**
- `internal/pty` (권장) 하위에 다음을 위치
  - `manager.go`: SessionManager (map + mutex)
  - `session.go`: Session struct, terminate logic
  - `resolve.go`: cwd resolution/workspace boundary validation
  - `errors.go`: 에러 코드 상수

2) **workspace 조회**
- 서버 state(기존)에서 `workspace_id`를 찾아 `local_path`를 얻는다.
- workspace가 없으면 `unknown_workspace`로 실패(HTTP 400 또는 404 중 하나로 고정; v1의 unknown_* 관례를 고려하면 400도 무난).

3) **cwd 정규화(중요)**
- `resolved = filepath.Clean(filepath.Join(local_path, cwd_rel))`
- 반드시 방어할 것
  - `cwd_rel`에 절대경로/드라이브 경로가 들어오는 경우
  - `..`로 탈출 시도
  - (권장) `EvalSymlinks` 적용 후 workspace 경계 비교

4) **workspace 경계 체크**
- 정책: resolved_cwd가 workspace `local_path` 하위면 허용
- 거부 시 `cwd_traversal_attempt` 또는 `absolute_path_not_allowed` (HTTP 400)

5) **세션 생성**
- 요청 `shell`은 allowed shells로 제한 가능
- PTY spawn 시 `Cmd.Dir = resolved_cwd`
- `cols/rows`가 있으면 초기 window size 반영(없으면 기본)

6) **세션 종료(좀비 방지의 핵심)**
- `Terminate(id)`는 반드시 다음을 수행
  - 프로세스 kill(필요 시 process group 포함)
  - `Wait()` 수행
  - PTY FD close
  - 세션 제거
- 멱등 종료(idempotent) 권장: 이미 종료됐거나 없는 id에도 “성공”으로 처리(프론트 단순화)

7) **응답 포맷**
- 최소한 다음을 반환
  - `session_id`
  - `ws_url` (아직 STASK-2에서 구현되지만 경로 문자열은 제공)
  - `resolved_cwd`

### 테스트/검증
- Unit
  - `ResolveCWD`의 탈출 방지 케이스
  - SessionManager 멱등 terminate
- Integration (`httptest`)
  - 임시 디렉토리를 workspace local_path로 설정
  - create → terminate 후 프로세스가 남지 않는지 확인(가능하면 `ProcessState`/`Signal(0)` 방식)

### 완료 기준
- create/terminate가 반복되어도 프로세스/FD 누수가 관측되지 않는다.
- workspace 경계를 벗어나는 cwd 요청은 항상 거부된다.

---

## STASK-2 — WS Attach (I/O streaming + Resize + Single-attach)

### 목적
- v3 핵심 플로우인 “WS attach + 스트리밍”을 완성한다.
- 클라이언트가 xterm에서 입력/출력을 실시간으로 사용할 수 있다.

### 포함 범위
- `GET (WS) /api/pty/sessions/{id}/ws`
- WS ↔ PTY 양방향 I/O
- Resize control message 처리
- single-attach 정책

### 구현 가이드
1) **WS 프레임 규약(권장)**
- binary frame: raw bytes
- text frame(JSON): control 메시지(resize), 서버 이벤트(exit/error)

2) **I/O 파이프**
- WS read loop:
  - binary → PTY write
  - text(JSON) → control 처리
- PTY read loop:
  - PTY read → WS binary send

3) **Resize**
- `{type:"resize", cols, rows}` 수신 시 PTY size 반영

4) **Single-attach**
- 세션에 `attached bool`(mutex로 보호)
- 이미 attached면 409 + `session_already_attached` 또는 WS close

5) **Backpressure**
- WS send가 지속적으로 막힐 때의 정책을 정한다.
  - 권장: 버퍼 제한 초과 시 세션 종료 + `ws_backpressure` 에러 통지

### 테스트/검증
- Integration(WebSocket)
  - create → ws connect → `echo __kkachi_test__\n` 전송 → 출력 수신
  - resize 메시지를 보내도 WS가 죽지 않음(최소 보장)
- Race
  - `go test -race ./...`에서 attach 관련 경쟁 없음

### 완료 기준
- 서버 단독으로 “echo roundtrip”이 재현된다.
- single-attach 정책이 결정된 방식대로 동작한다.

---

## STASK-3 — Lifecycle Hardening (disconnect 정책 고정 + exit 처리 + limits/logging)

### 목적
- v3 Exit Criteria의 핵심인 **정리 정책 일관성**과 **좀비 방지 회귀 차단**을 테스트로 고정한다.
- 운영 중 runaway/누적을 막는 가드레일(세션 제한, 최소 로그)을 추가한다.

### 포함 범위
- disconnect 정책 결정 및 구현
  - `terminate_immediately` (권장 기본)
  - 또는 `idle_timeout` (옵션)
- 프로세스 자연 종료(exit) 처리 및 WS 통지
- 리소스 제한(최대 세션 수 등) 옵션
- 최소 로깅(이벤트 단위)

### 구현 가이드
1) **disconnect 정책**
- STASK-2의 WS handler에서 `OnClose`/read-loop 종료 시
  - `terminate_immediately`: 즉시 `Terminate(session)`
  - `idle_timeout`: deadline 설정 후 만료 시 terminate
- 정책은 설정 값으로 고정하고 “부분적으로 다른 동작”이 없게 한다.

2) **프로세스 자연 종료**
- child process 종료 감지
- (가능하면) WS에 `{type:"exit", exit_code,...}` 전송 후 close
- 세션은 반드시 terminate/cleanup

3) **세션 제한(권장)**
- 설정: `max_sessions_per_client` 또는 `max_sessions_total`
- 초과 시 429 또는 400으로 고정(프론트 메시지 매핑 가능)

4) **로깅**
- created / attached / detached / terminated / blocked 등을 구조화 로그로 남김

### 테스트/검증
- Integration
  - WS close(클라이언트) → 서버가 정책대로 세션을 종료하는지 검증
  - `exit\n` 입력 → exit 이벤트/cleanup 검증
  - 세션 제한 도달 케이스 검증

### 완료 기준
- disconnect 시 정리 정책이 항상 동일하게 적용된다.
- 좀비/FD 누수가 회귀 테스트로 잠긴다.

---

## STASK-4 — Command Blacklist (server-side guardrail)

### 목적
- v3 보안 최소 요구사항(초기 가드레일)인 위험 명령 blacklist를 서버에서 제공한다.

### 포함 범위
- blacklist 패턴 설정(기본값 + override)
- stdin best-effort 라인 버퍼링 후 패턴 매칭
- 차단 시 PTY로 전달하지 않고 client에 `command_blocked` 통지

### 구현 가이드
1) **정확성 목표**
- interactive shell 특성상 100% 완벽한 차단은 불가능하므로, “가드레일”로 정의한다.

2) **매칭 단위**
- `\n`(Enter) 기준으로 라인을 구성하여 매칭(최소 구현)
- 패턴은 substring 또는 regexp(운영 요구에 따라)로 구성

3) **차단 동작**
- 차단된 라인은 PTY에 쓰지 않는다.
- WS에 `{type:"error", error:"command_blocked", reason:"..."}` 전송

### 테스트/검증
- Integration
  - 차단 패턴 입력 → `command_blocked` 수신
  - 이후 정상 명령(`echo ok`) 동작 확인

### 완료 기준
- blacklist on/off가 설정으로 제어된다.
- 최소한 대표 위험 패턴이 차단된다.

---

## STASK-5 — Auth Scaffolding (optional token auth default-off) + Final polish

### 목적
- 옵션 토큰 인증을 “붙일 수 있는 구조”로 제공하되, 기본값은 off로 유지한다.
- v3 배포 품질을 위해 최종 폴리시(문서/에러/테스트)를 정리한다.

### 포함 범위
- auth 설정
  - `auth_enabled: false` 기본
  - `auth_token` (enabled 시 필수)
- 적용 범위
  - 최소: `/api/pty/*` 보호
- HTTP + WS 인증
- 폴리시 마감
  - API 문서(간단 README)
  - 에러 코드/HTTP status 일관화

### 구현 가이드
1) **HTTP 인증**
- `Authorization: Bearer <token>`

2) **WS 인증**
- 브라우저는 WS에 커스텀 헤더 전송이 제한될 수 있음
- 아래 중 **하나로 고정**
  - (A) query param: `wss://.../ws?token=...`
  - (B) cookie 기반
- 선택한 방식은 requirement.md의 계약 섹션을 업데이트하고, CTASK-5에서 클라이언트에 반영한다.

3) **기본 off 유지**
- auth disabled일 때는 v2와 동일하게 “추가 설정 없이” 동작해야 한다.

### 테스트/검증
- Auth disabled(default)
  - 기존 모든 테스트 통과
- Auth enabled
  - HTTP/WS 모두 토큰 없으면 거부
  - 토큰 있으면 정상 동작

### 완료 기준
- 인증이 켜져도/꺼져도 회귀 없이 동작한다.
- WS 인증 방식이 클라이언트와 합의된 형태로 문서화된다.
