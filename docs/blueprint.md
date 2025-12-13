## 0. 공통 전제 및 설계 원칙 (v2~v20 공통)

### 0.1 v1(기존) 기반을 훼손하지 않는다
- Kkachi의 근간은 v1의 **중앙 docs repo 조정(kkachi-server)** + **workspace CLI + Git hooks** 워크플로우다.
- docs repo에 “새 커밋을 만드는 모든 쓰기 경로”는 **docs_repo_id 단위 mutex**로 직렬화되어야 한다.
  - `/docs/push` 뿐 아니라, 이후 버전에서 도입되는 **TaskDoc 생성 커밋**, **Run 로그 커밋** 등도 동일한 규칙을 따른다.

### 0.2 Source of Truth(SOT) 경계
- **Docs Repo(SOT, 내용/이력):**
  - 문서 본문, TaskDoc, Run 로그 문서, 결정/설계/결론 등 “업무의 실체”는 Git 히스토리에 남긴다.
- **DB(SOT, 운영 메타):**
  - 보드/필터/정렬에 필요한 운영 메타(상태, assignee, 라벨, 검색 인덱스, 의존 관계, Run 메타 등)
- “문서 기반 운영”을 지키되, **운영 메타 변경이 문서 커밋 노이즈로 폭증하지 않도록** 분리한다.

### 0.3 Task 철학(경량 Jira의 핵심)
- Task는 항상 **TaskDoc(Markdown)** 를 동반한다.
  - 기본 경로: `tasks/<project>/<task_id>.md`
- 완료(Done)된 TaskDoc은 **절대 수정 불가** (서버 레벨에서 강제).
  - 변경이 필요하면 “기존 Task 수정”이 아니라 **새 Task 생성 + 링크**가 표준이다.

### 0.4 API/라우팅 일관성
- Web 전용 API는 **모두 `/api/*`** 프리픽스를 사용한다.
- SPA 라우팅은 “API/asset 제외하고 index.html”로 일관되게 처리한다.
  - (예) `/api/*` → API, `/assets/*` → 정적 assets, 그 외 `/*` → SPA index.html

### 0.5 보안/운영 기본값
- 기본 전제는 로컬 네트워크 내부(개인/소규모 팀).
- 다만, 터미널/에이전트/MCP는 공격면이 크므로:
  - v3부터 “옵션 토큰 인증(기본 off)”을 구조적으로 수용
  - 위험 명령어 blacklist는 최소한 제공

### 0.6 릴리즈 원칙
- 각 버전은 “배포 가능한 단위(동작/문서/테스트)”로 완결되어야 한다.
- **기능은 얇게**, 대신 “SOT 경계/동시성/무결성”은 초기에 잡는다.

---

## 1. 로드맵 요약 (한 줄 버전 정의)

| Version | Theme | Primary Outcome |
|---|---|---|
| v2 | Web Dashboard (Read-only) | 상태 가시화 + 확장 가능한 라우팅/API 베이스 |
| v3 | Web Terminal / PTY | 원격 개발(쉘/AI CLI) 기반 확보 |
| v4 | Task & TaskDoc + SQLite | 경량 Jira 코어(문서 기반 Task) + 상태 저장소 전환 |
| v5 | Kanban | Task를 “운영 가능한 수준”의 보드로 제공 |
| v6 | Dependencies + Done immutability | 작업 순서 규율 + TaskDoc 불변성 서버 강제 |
| v7 | Agent & Run & Logs | Task 실행/로그 플랫폼화(원격 개발 체계화) |
| v8 | MCP Client (Config UI) | 외부 MCP 연결/툴 매핑(Agent 강화) |
| v9 | Kkachi MCP Server | Kkachi를 외부 LLM/IDE에 “도구로” 노출 |
| v10 | Remote Dev Workspace Model | 실행 컨텍스트(Repo/Checkout/CWD) 표준화 |
| v11 | Isolation & Limits | 실행 격리/리소스 제한/안전성 강화 |
| v12 | Review & Diff UX | Run 결과/Docs diff를 웹에서 검토 가능 |
| v13 | Auth/RBAC/Audit | 운영 하드닝(토큰/역할/감사로그) |
| v14 | Unified Search (FTS) | Tasks/Docs/Runs 통합 검색 |
| v15 | Automation & Notifications | Webhook/알림/정책 엔진 |
| v16~v20 | Optional Scale-Ups | 스케줄링, 편집기, 플러그인화, 멀티 인스턴스 등 |

---

## 2. v2 – Web Dashboard (Read-only) [수정/보강 버전]

### 2.1 목표
- v1 서버의 `/state`를 기반으로 project/workspace 상태를 **읽기 전용**으로 시각화한다.
- v3 `/terminal`, v4 `Tasks` 화면 확장을 막지 않는 “UI/라우팅/프론트 구조”를 선제 확정한다.

### 2.2 기능 범위 (MVP)
- Project 리스트(요약): docs head, workspace 수, outdated 수, last_reported_at max
- Project 상세: workspace 테이블 + up-to-date/outdated badge
- Raw state(선택): `/api/state` JSON 뷰어
- 공통: 수동 refresh, 로딩/에러/빈상태 처리

### 2.3 v2에서 확정하는 변경(현 문서 대비 보강)
- **`/api/state`는 “옵션”이 아니라 v2부터 “필수”로 제공**(프론트 fallback 제거)
- SPA 라우팅은 `/app/*` 같은 일부 패턴이 아니라, **API/asset 제외 catch-all**로 통일
- UI 레이아웃은 “Projects / ProjectDetail / Terminal / Tasks(미노출)” 탭 확장 가능 구조로 구현

### 2.4 비범위
- Web에서 docs 편집/Push 수행 금지
- Task/Agent/Run/MCP 기능은 UI에 “자리만” 확보하고 기능은 배제

### 2.5 완료 기준(Exit Criteria)
- 최초 로딩 1회 `/api/state` 호출로 주요 화면 렌더링 가능
- Project/Workspace 상태 계산이 v1 CLI status 개념과 모순되지 않음
- SPA direct URL 진입(새로고침) 시 404 없이 정상 진입

---

## 3. v3 – Web Terminal & PTY (원격 개발 기반)

### 3.1 목표
- Web에서 단일 PTY 세션을 띄워 **쉘/AI CLI 실행**이 가능하게 한다.
- 이후 v7 Run/Agent의 기반이 되는 “원격 실행 인프라”를 확보한다.

### 3.2 기능 범위 (MVP)
- `/terminal` 페이지
- “세션 생성” → WebSocket 연결 → xterm.js attach
- 입력/출력 스트리밍, “세션 종료”
- 기본 실행 컨텍스트 지정(최소 1개):
  - (권장) `project` 또는 `cwd` 파라미터(서버 allowlist 경로만)

### 3.3 서버/인프라
- `creack/pty` 기반 PTY 세션 생성
- In-memory session manager (서버 재시작 시 소멸)
- WebSocket endpoint: `/api/pty/sessions` + `/api/pty/sessions/{id}/ws`

### 3.4 보안(최소)
- blacklist 기반 위험 명령 차단(초기)
- 옵션 토큰 인증(기본 off)용 middleware 스캐폴딩만 준비

### 3.5 비범위
- 세션 복구/재연결, 로그 영속화(본격은 v7)
- Task/Docs와 PTY 연결(이 단계에서는 분리)

### 3.6 완료 기준
- 좀비 프로세스/리소스 누수 없이 세션 종료 가능
- 연결 끊김 시 서버 측 정리 정책이 일관됨(Idle timeout 또는 즉시 종료)

---

## 4. v4 – Task & TaskDoc + SQLite (경량 Jira 코어)

### 4.1 목표
- “문서 기반 Task 시스템(경량 Jira)”의 코어를 도입한다.
- 서버 state 저장소를 JSON → SQLite로 전환하여 이후 확장 기반을 만든다.

### 4.2 기능 범위 (MVP)
1) Task 생성
- 입력: project, title, (optional) description
- 결과:
  - DB에 Task 메타 생성
  - docs repo에 TaskDoc 생성 + Git commit
    - `tasks/<project>/<task_id>.md`
    - 템플릿: 목적/범위/수용기준/링크 섹션

2) Task 조회
- 프로젝트별 Task 리스트(필터: status, assignee는 v5에서 강화 가능)
- Task 상세:
  - DB 메타 + TaskDoc viewer(최소 read-only) 또는 외부 링크

3) State 저장소 전환
- v1 state(JSON) → SQLite 마이그레이션 도구 제공(서버 subcommand)

### 4.3 서버/인프라 핵심 규율
- **docs repo에 커밋을 만드는 모든 경로는 docs_repo_id mutex를 반드시 사용**
  - TaskDoc 생성 커밋 역시 `/docs/push`와 동일한 직렬화 규칙
- “Git 작업 시퀀스( fetch → reset → 변경 → commit → push )”는 단일 원자적 흐름으로 유지

### 4.4 데이터 모델
- SQLite (예시)
  - `workspaces`, `projects`, `docs_repos` (기존 state 이관)
  - `tasks`:
    - `id`, `project`, `title`, `status(todo|in_progress|done)`,
      `assignee`, `task_doc_path`, `created_at`, `updated_at`, `completed_at`
- 원칙: **status는 DB가 SOT**, TaskDoc에 status를 중복 기록하지 않는다(커밋 노이즈 방지).

### 4.5 Web UI/UX
- `/projects/:project/tasks` (리스트)
- `/tasks/:task_id` (상세 + TaskDoc 보기)

### 4.6 비범위
- Kanban(v5), 의존관계/불변성(v6), Agent/Run(v7)

### 4.7 완료 기준
- Task 생성이 “DB + TaskDoc commit”까지 원자적으로 완료(실패 시 롤백/재시도 전략 존재)
- SQLite 마이그레이션이 단일 명령으로 재현 가능

---

## 5. v5 – Kanban Board (경량 Jira 사용성 완성)

### 5.1 목표
- Task를 칸반 보드로 운영 가능하게 만든다(드래그&드롭).

### 5.2 기능 범위 (MVP)
- 컬럼: `ToDo / In Progress / Done`
- 카드: task_id, title, assignee
- Drag & Drop → status 변경(PATCH)
- title/assignee 간단 수정

### 5.3 정책
- Kanban status 변경은 **TaskDoc 자동 편집을 하지 않는다**(문서 커밋 노이즈 방지).

### 5.4 데이터 모델
- `tasks.assignee` 확정
- (선택) `tasks.rank` 또는 `tasks.position` (동일 상태 내 정렬)

### 5.5 완료 기준
- 보드 업데이트가 빠르고 일관됨(Optimistic UI + 실패 롤백)
- status 변경이 문서 커밋을 발생시키지 않음

---

## 6. v6 – Dependencies + Done TaskDoc 불변성(서버 강제)

### 6.1 목표
- 작업 순서(의존 관계)를 모델링하고 Done 규율을 강제한다.
- “완료된 TaskDoc은 절대 수정 불가”를 **서버 레벨**에서 강제한다.

### 6.2 기능 범위 (MVP)
1) 의존 관계
- `task_dependencies(task_id, depends_on_task_id)`
- 규칙: 선행 Task가 done 아니면 후행 Task를 done으로 전이 불가

2) Done TaskDoc 불변성
- docs write 경로에서, 변경된 파일 목록 중 `tasks/<project>/<task_id>.md` 변경을 검사
- 해당 task가 done이면 push 거부(예외 없음)

### 6.3 구현 포인트
- 불변성 강제는 HTTP handler가 아니라 **DocsWriteRepository.PushSnapshot 내부**에서 강제
- Run 로그 파일(`tasks/<project>/<task_id>/runs/*.md`) 추가는 허용(불변성과 분리)

### 6.4 완료 기준
- 의존 관계 위반 시 Done 전이 거부 + 명확한 오류 메시지
- done TaskDoc 변경 시 어떤 경로에서도 서버가 거부(우회 불가)

---

## 7. v7 – Agent & Run & 로그(실행 플랫폼화)

### 7.1 목표
- Task 중심으로 실행을 표준화(Agent/Run)하고, 실행 로그를 docs repo에 남긴다.
- “원격 개발”을 단순 터미널이 아니라 **재현 가능한 실행 이력**으로 승격한다.

### 7.2 기능 범위 (MVP)
1) Agent
- `agents`: id, name, cli_command(또는 type), default_prompt_template
- Web에서 Agent 생성/수정/테스트 실행

2) Run
- `runs`: id, task_id, agent_id, status(pending/running/succeeded/failed),
  started_at, finished_at, log_doc_path
- 실행 흐름:
  - Task 상세 → Agent 선택 → Run 생성
  - 서버가 PTY 프로세스로 CLI 실행
  - 로그 실시간 스트리밍 + 디스크 스풀(서버 크래시 대비)
  - 종료 시 로그를 Markdown으로 정리해 docs repo 커밋:
    - `tasks/<project>/<task_id>/runs/<run_id>.md`

3) UI
- Task 상세에 Runs 탭(리스트/상태/로그 링크)
- 실행 중 로그 스트리밍 화면

### 7.3 정책
- Run 로그는 “요약 + 핵심” 중심(레포 비대화 방지)
- TaskDoc 불변성 정책과 충돌하지 않게 로그는 별도 경로에 저장

### 7.4 완료 기준
- Run 생성→실행→로그 커밋→상태 업데이트까지 end-to-end 동작
- 서버 재시작/중단 시에도 “실행 중 로그 유실 최소화” 전략 존재(스풀)

---

## 8. v8 – MCP Client (설정/연결 UI)

### 8.1 목표
- Kkachi가 외부 MCP 서버를 소비할 수 있게 하고,
  Agent가 사용할 MCP Tool을 UI에서 구성한다.

### 8.2 기능 범위 (MVP)
- MCP 서버 등록/편집(이름, endpoint, auth)
- Tool 목록 동기화(Discovery)
- Agent ↔ Tool 매핑

### 8.3 데이터 모델
- `mcp_servers`, `mcp_tools`, `agent_mcp_tools`

### 8.4 완료 기준
- 특정 MCP 서버를 등록하고, Tool 목록을 가져와 Agent에 연결한 뒤 Run에서 활용 가능(최소 1개 사례)

---

## 9. v9 – Kkachi MCP Server (외부에 Kkachi를 Tool로 제공)

### 9.1 목표
- 외부 LLM/IDE가 MCP를 통해 Kkachi의 Task/Run/Docs/State를 호출하게 한다.

### 9.2 기능 범위 (MVP Tool Groups)
- docs.* (읽기): head/snapshot 메타 등
- task.* : list/get/create/update_status/add_dependency
- agent.* / run.* : agent_list, run_create, run_get
- state.* : state_get(project optional)

### 9.3 정책
- **docs 쓰기 직접 노출 금지**
  - MCP로 `/docs/push` 역할을 직접 제공하지 않음
  - 문서 변경은 “task + run”의 간접 경로를 기본으로

### 9.4 구현
- MCP는 새로운 interface 레이어로 구현(`interface/mcp` → usecase 호출)
- 배포는 별도 바이너리(`kkachi-mcp`) 우선, 필요 시 통합 옵션 제공

### 9.5 완료 기준
- 외부 MCP 클라이언트에서 task 생성/조회, run 생성/조회가 end-to-end로 동작

---

## 10. v10 – Remote Dev Workspace Model (실행 컨텍스트 표준화)

### 10.1 목표
- “터미널/런이 어느 repo/브랜치/디렉토리에서 실행되는가”를 제품적으로 모델링한다.

### 10.2 기능 범위 (MVP)
- execution workspace(가칭) 등록:
  - project, repo_url, local_path(or checkout root), default_ref
- Terminal/Run 생성 시 workspace 선택
- 최소한 “프로젝트별 실행 위치의 표준화” 제공

### 10.3 완료 기준
- 같은 project에서 누구나 동일한 실행 컨텍스트로 터미널/런을 재현 가능

---

## 11. v11 – Isolation & Resource Limits (안전한 원격 실행)

### 11.1 목표
- 원격 실행의 안전성/안정성 확보(리소스 제한, 실행 격리).

### 11.2 기능 범위 (MVP)
- 실행 시간 제한/강제 종료
- 메모리/CPU 제한(가능한 범위 내)
- (선택) 컨테이너 기반 격리 옵션(초기에는 feature flag)

### 11.3 완료 기준
- runaway 프로세스가 시스템을 망가뜨리지 않도록 가드레일 적용

---

## 12. v12 – Review & Diff UX (Run 결과 검토/승인 흐름)

### 12.1 목표
- Run이 만든 결과(문서 변경)를 “웹에서 검토 가능한 형태”로 만든다.

### 12.2 기능 범위 (MVP)
- Run 완료 후:
  - docs repo diff(변경 파일 목록 + 요약)
  - 커밋 링크/해시 표시
- Task 상세에서 “최근 변경/Run 결과” 요약 제공

### 12.3 완료 기준
- 사용자가 CLI 없이도 “무엇이 바뀌었는지” 빠르게 확인 가능

---

## 13. v13 – Auth/RBAC/Audit (운영 하드닝)

### 13.1 목표
- 터미널/MCP/실행 기능을 안전하게 운영 가능하게 한다.

### 13.2 기능 범위 (MVP)
- 토큰 기반 인증(최소)
- 역할: viewer / operator / admin (최소 3단계)
- Audit log:
  - task 생성/상태변경
  - run 생성/실행
  - terminal 세션 생성/종료
  - mcp 호출(요약)

### 13.3 완료 기준
- 누가 무엇을 했는지 추적 가능 + 최소 권한 운영 가능

---

## 14. v14 – Unified Search (FTS)

### 14.1 목표
- “문서+티켓+로그 통합 관리”의 체감 가치를 검색으로 제공한다.

### 14.2 기능 범위 (MVP)
- SQLite FTS 기반 검색:
  - Task 제목/요약, TaskDoc(선택), Run 로그 요약
- 필터:
  - project/status/assignee/date range

### 14.3 완료 기준
- 운영자가 “관련 Task/Run을 10초 내 찾을 수 있는” 수준의 검색 UX 제공

---

## 15. v15 – Automation & Notifications (알림/정책 엔진)

### 15.1 목표
- 운영 자동화로 “붙어 있는 시스템”이 되게 한다.

### 15.2 기능 범위 (MVP)
- 웹훅/알림:
  - Task 생성/Done 전이/Run 완료/실패
- 정책 엔진(최소):
  - 특정 프로젝트는 Done 전이 제한 강화
  - 특정 Agent 실행 제한
- (선택) 스케줄링은 v16으로 분리

### 15.3 완료 기준
- Slack/웹훅 등 외부로 이벤트를 안정적으로 발행

---

## 16~20. Optional Scale-Ups (선택 확장)

### v16 – Scheduling / Batch Runs
- 특정 Task/Project에 대해 cron-like 실행/점검
- 비용/리스크가 크므로 운영 하드닝(v13) 이후 권장

### v17 – Web Doc Viewer 고도화(주석/하이라이트)
- “편집”이 아니라 “리뷰/주석” 중심으로 시작
- Done 불변성 정책과 충돌하지 않게 설계

### v18 – Pluggable Policies
- 조직별 정책(명령어 제한, 승인 단계 등)을 플러그인/스크립트로 주입 가능

### v19 – Multi-instance / Federation
- project 그룹별 서버 인스턴스 분리 운영 + 상위 통합 뷰(선택)

### v20 – Observability & Telemetry
- 실행/에러/성능 지표 대시보드
- MCP 호출/에이전트 실행의 비용/효율 분석

---

## 부록 A. “경량 Jira”를 경량으로 유지하기 위한 금지 목록
- 커스텀 워크플로우/필드 무한 확장(초기 금지)
- 스프린트/번다운/타임트래킹(초기 금지)
- 댓글/활동로그 DB화(가능한 TaskDoc로 흡수)
- status 변경마다 문서 커밋 생성(금지)

---

## 부록 B. 운영/백업 가이드(요약)
- docs repo는 Git으로 백업(원격 origin)
- SQLite DB는 주기적 스냅샷 백업(상태/운영 메타 SOT)
- DB 유실 시:
  - TaskDoc 스캔으로 기본 Task 목록/경로 복구 가능(단, status/assignee 등 운영 메타는 백업 필요)

---
