# Kkachi Web v3 Requirement (Web Terminal / PTY)

> v3 구현 시 “큰 그림 + 계약(Contract)” 문서.  
> - 목표/범위/비범위
> - API/WS 프로토콜(클라-서버 계약)
> - 세션 라이프사이클/정리 정책(Exit Criteria 직결)
> - 보안 최소 요건(workspace 경계/blacklist/auth scaffolding)
> - STASK/CTASK 태스크 맵(= PR tag 이름)

---

## 1. 목적

v3의 목표는 Web에서 PTY 기반 터미널을 열어 **쉘/AI CLI 실행이 가능**하도록 하고, 이후 v7 Run/Agent의 기반이 되는 “원격 실행 인프라”의 최소 골격을 확보하는 것이다.

---

## 2. 공통 전제 / 설계 원칙

### 2.1 v1 기반 불변
- v3는 v1의 docs repo 조정 워크플로우(서버 + CLI + hooks)에 영향을 주지 않는다.
- PTY 기능은 **docs repo write path(/docs/push 등)와 분리된 코드 경로**로 구현한다.

### 2.2 API/라우팅 일관성
- Web 전용 API는 **모두 `/api/*` prefix**를 사용한다.
- SPA 라우팅은 **`/api/*`, `/assets/*` 제외 catch-all** 원칙을 유지한다.

### 2.3 보안 기본값
- 운영 전제는 로컬 네트워크(개인/소규모 팀)이다.
- 다만 터미널은 공격면이 크므로 v3부터:
  - **옵션 토큰 인증(기본 off)** 을 구조적으로 수용하고
  - **위험 명령 blacklist** 를 최소 제공한다.

### 2.4 릴리즈 원칙
- v3는 “배포 가능한 단위(동작/문서/테스트)”로 완결되어야 한다.
- 기능은 얇게 유지하되, **무결성/정리(좀비 방지)와 정책 일관성**은 초기에 고정한다.

---

## 3. 범위

### 3.1 Web UI 범위
- `/terminal` 페이지 제공
- Workspace 선택 → 해당 디렉토리에서 콘솔 오픈
- 콘솔 복수 생성/전환
- 좌측 콘솔 리스트를 드래그&드롭으로 순서 변경
- 각 콘솔의 입력/출력 스트리밍, 세션 종료

### 3.2 Server/API 범위
- `creack/pty` 기반 PTY 생성
- In-memory session manager
- 엔드포인트:
  - `POST /api/pty/sessions`
  - `GET (WS) /api/pty/sessions/{id}/ws`
  - `DELETE /api/pty/sessions/{id}` (세션 종료)
- 실행 컨텍스트 최소 1개: **cwd**
  - workspace 기반으로 cwd를 해석하되, **workspace local_path 경계 안에서만 허용**
- 보안 최소:
  - 위험 명령 blacklist(가드레일)
  - 옵션 토큰 인증(기본 off) 스캐폴딩
- 정리 정책(Exit Criteria):
  - 좀비/리소스 누수 없이 종료
  - disconnect 시 정리 정책 일관(즉시 종료 또는 idle timeout 중 하나로 고정)

---

## 4. 비범위(명시적 제외)

- 세션 복구/재연결(리로드 후 이전 세션 재부착 등)
- 로그 영속화(터미널 출력 저장/검색)
- Task/Docs와 PTY 연결(터미널에서 Task 실행으로 연결 등)

---

## 5. 사용자 시나리오 / 수용 기준

### 5.1 시나리오
1) 사용자는 `/terminal`로 이동한다.
2) `New Console` → Workspace 선택 → 선택한 workspace 디렉토리에서 쉘이 열린다.
3) 사용자는 여러 콘솔을 생성하고 좌측 리스트에서 전환한다.
4) 사용자는 드래그&드롭으로 콘솔 순서를 바꾼다.
5) 사용자는 콘솔을 닫아 세션을 종료한다.

### 5.2 Exit Criteria(필수)
- (EC-1) 세션 종료 시 **좀비 프로세스/FD 누수 없이** 종료된다.
- (EC-2) WS 연결 끊김 시 서버 정리 정책이 **일관**되게 적용된다.
  - `terminate_immediately` 또는 `idle_timeout` 중 하나를 선택해 고정.
- (EC-3) 세션 생성 → WS attach → 스트리밍 → 종료가 end-to-end로 동작한다.

---

## 6. 아키텍처(개념)

### 6.1 데이터 소스
- Workspace 목록은 `GET /api/state`(= `/state` alias)에서 제공되는 `workspaces[]`를 사용한다.
- 각 workspace는 `workspace_id`, `project`, `local_path` 등을 포함한다.

### 6.2 PTY 세션 모델
- “콘솔(클라이언트)” 1개 ↔ “PTY 세션(서버)” 1개
- 서버 세션은 in-memory로만 유지되며 서버 재시작 시 소멸한다.

---

## 7. API/WS 프로토콜 (클라-서버 계약)

> 아래 계약은 v3 구현의 기준선이다.  
> 단, auth 토큰 전달 방식(특히 WS)은 STASK-5/CTASK-5에서 서버 구현과 합의 후 확정한다.

### 7.1 `POST /api/pty/sessions` (세션 생성)

#### 요청(JSON)
```json
{
  "workspace_id": "sudal:/Users/karl/dev/sudal",
  "cwd_rel": "", 
  "title": "sudal",
  "shell": "bash",
  "cols": 120,
  "rows": 30
}
```

- `workspace_id` (필수): `/api/state`에서 선택
- `cwd_rel` (선택, default=""): workspace root 대비 상대 경로
- `title` (선택): UI 표기용
- `shell` (선택): 허용 쉘 목록으로 제한 가능
- `cols/rows` (선택): 초기 터미널 크기

#### 서버 동작
- `workspace_id`로 state에서 workspace를 찾고 `local_path`를 root로 사용
- `resolved_cwd = Clean(Join(local_path, cwd_rel))`
- workspace 경계 검증 실패 시 거부
- PTY 생성 후 세션 등록

#### 응답(JSON)
- 성공(201 Created)
```json
{
  "session_id": "pty_01HZ...",
  "ws_url": "/api/pty/sessions/pty_01HZ.../ws",
  "resolved_cwd": "/Users/karl/dev/sudal"
}
```
- 인증 실패(401 Unauthorized): `auth_enabled=true`인데 토큰이 없거나 틀린 경우
- 요청 오류(400 Bad Request): `unknown_workspace`, `cwd_traversal_attempt`, `invalid_terminal_size` 등
- 제한 초과(429 Too Many Requests): `session_limit_exceeded`

### 7.2 `DELETE /api/pty/sessions/{id}` (세션 종료)
- 멱등(idempotent) 동작을 권장한다.
- 종료 시 반드시:
  - child process kill
  - `Wait()`로 프로세스 종료 수거
  - PTY FD close
  - 세션 매니저에서 제거

### 7.3 `GET (WebSocket) /api/pty/sessions/{id}/ws` (세션 attach)

#### attach 정책
- v3 기본 정책: **세션당 WS 1개만 허용**(중복 attach는 409 `session_already_attached` 등으로 거부).
- v3는 “재연결/복구”가 비범위이므로, 중복 attach 허용(기존 연결 강제 종료 후 새 연결)은 가급적 피한다.

#### 인증 (Auth)
- `auth_enabled=true`일 때, 요청 헤더에 유효한 `auth_token` 쿠키가 포함되어야 함.
- 인증 실패 시 **401 Unauthorized** 반환.

#### Origin 정책
- 기본: **same-origin**만 허용(요청 Host와 Origin의 host:port 일치).
- 예외: `PTY_WS_ALLOWED_ORIGINS` allowlist에 포함된 Origin은 허용.
- `Origin` 헤더가 없거나 `null`이면 거부(403).
- allowlist는 `http(s)://host[:port]` 형태만 허용(정확 매칭).

#### WS 프레임 규약(권장)
- **binary frame**: raw bytes
  - client → server: stdin
  - server → client: stdout/stderr
- **text frame(JSON)**: 제어 메시지

##### Resize (client → server)
```json
{ "type": "resize", "cols": 120, "rows": 30 }
```

##### Exit notify (server → client)
```json
{ "type": "exit", "exit_code": 0, "signal": "" }
```

##### Error notify (server → client)
```json
{ "type": "error", "error": "command_blocked", "reason": "..." }
```

---

## 8. 라이프사이클/정리 정책

### 8.1 disconnect 정책 (v3 고정)
- WS 연결이 끊기면 **즉시 세션 종료(terminate-only)**.
- stay/idle timeout 기반 재연결 정책은 v3 비범위.

### 8.3 프로세스 자연 종료
- 쉘이 `exit` 등으로 자연 종료되면 서버는:
  - 세션 종료 처리(정리)
  - WS에 exit 이벤트 통지(가능한 경우)

---

## 9. 보안 최소 요건

### 9.1 CWD workspace boundary(필수)
- `resolved_cwd`는 workspace `local_path` 하위 경로만 허용
- 경로 정규화/탈출 방지(`..`, 절대경로 override, symlink 등)

### 9.2 위험 명령 blacklist(필수, 가드레일)
- “완벽한 보안 경계”가 아니라 초기 가드레일
- 서버는 stdin best-effort 라인 버퍼링 후 패턴 매칭
- 매칭 시:
  - 해당 라인을 PTY로 전달하지 않음
  - 클라이언트에 `command_blocked` 통지(JSON error + 터미널 출력 안내)

### 9.3 옵션 토큰 인증(기본 off) 스캐폴딩
- 서버 설정으로 `auth_enabled=false` 기본
- on일 때:
  - HTTP: `Authorization: Bearer <token>`
  - WS: **Cookie 기반 인증 (`auth_token` 쿠키)** 으로 확정 (STASK-5에서 구현 완료)

---

## 10. 관측/로깅(권장)

서버는 최소한 아래 이벤트를 로그로 남긴다.
- session created (workspace_id, resolved_cwd, session_id)
- ws attached / detached
- session terminated (reason=user_close|disconnect|exit|error)
- command blocked

---

## 11. 테스트 기대치

### 11.1 서버
- unit: path resolution(workspace 경계), session manager 멱등 종료, blacklist 매칭, auth middleware
- integration:
  - HTTP create/terminate
  - WS attach + roundtrip(`echo` 출력 확인)
  - disconnect 시 정리 정책 검증(Exit Criteria 잠금)

### 11.2 클라이언트
- unit: 상태 전이(add/remove/select/reorder), DnD reorder 알고리즘
- component: workspace picker → create session 호출 → 콘솔 항목 생성
- e2e(권장): echo roundtrip, multi-console 전환, reorder

---

## 12. 태스크 맵(= PR Tag 이름)

### 12.1 Server Tasks
- **STASK-1**: PTY Foundation (SessionManager + HTTP create/terminate + CWD workspace boundary)
- **STASK-2**: WS Attach (I/O streaming + resize + single-attach policy)
- **STASK-3**: Lifecycle Hardening (disconnect 정책 고정 + exit 처리 + limits/logging)
- **STASK-4**: Command Blacklist (server-side guardrail)
- **STASK-5**: Auth Scaffolding (optional token auth default-off + polish)

### 12.2 Client Tasks
- **CTASK-1**: `/terminal` 스캐폴딩 + 상태 모델 + API 모듈 골격
- **CTASK-2**: Workspace Picker + 세션 생성(HTTP) + 콘솔 리스트(WS 없이)
- **CTASK-3**: xterm + WS attach + 스트리밍(단일 콘솔 E2E)
- **CTASK-4**: Multi-console(복수 세션) + 전환(연결 유지) + 정리
- **CTASK-5**: DnD reorder + (선택)정렬 저장 + Auth 토큰 대응 + UX/테스트 마감

### 12.3 의존 관계(권장)
- CTASK-2는 STASK-1 이후 진행
- CTASK-3는 STASK-2 이후 진행
- CTASK-4는 STASK-2 이후 가능(정리 정책은 STASK-3과 합의되면 안정)
- CTASK-5의 auth 부분은 STASK-5 구현 방식 확정 후 맞춤
