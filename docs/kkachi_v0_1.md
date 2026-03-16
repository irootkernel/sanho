# kkachi v0.1 요구사항 정리

## 1. 목적 및 범위

**kkachi(까치)**는 OpenCode를 실행 엔진(Execution Plane)으로 사용하고, 그 위에 Web 기반 컨트롤 플레인(Control Plane)을 구축하는 제품이다.

핵심 목표는 다음과 같다.

- 사용자의 목표를 인터뷰로 구조화해 **Spec, Track, Task**로 설계하고, **승인(Approval)**을 받은 뒤 구현을 진행한다.
- 멀티 리포(server, client, docs)를 하나의 Workspace로 묶어, 코드와 문서 및 작업 이력을 함께 운영한다.
- **Moderator 중심 작업**을 기본으로 하되, 다수의 **worker session**을 병렬로 운용(fan-out, join, retrigger)하여 조사, 검증, 구현 효율을 높인다.
- **Sub-moderator**로 계획 비평, 리스크 지적, 충돌 중재를 수행해 Ralph loop의 반복 횟수를 줄인다.
- lint, validator, test, LSP diagnostics 기반의 **Gate**와 **Ralph loop**를 결합해 “끝까지 완료”를 자동화한다.
- 완료된 결과물과 로그는 **불변(immutable) history**로 누적하고, SQLite 기반으로 검색 및 분석할 수 있게 한다.

비목표(Non-goals)

- `oh-my-opencode` 번들(에이전트/훅/스킬/설정)을 가져다 쓰지 않는다.

## 2. 핵심 개념(엔티티)

- **Workspace**: 여러 Repo를 묶는 작업 단위. 사용자, 모델 라우팅, Gate, 정책을 포함한다.
- **Repo**: server, client, docs 등 개별 저장소.
- **Spec**: 요구사항 및 성공 조건, 제약, 리스크를 담는 최상위 문서.
- **Track**: Spec을 구현하기 위한 큰 작업 흐름(에픽 수준). Task 그래프를 포함한다.
- **Task**: 실행 가능한 최소 작업 단위(TASK-283 등). repo scope, gate 조건, loop 조건을 포함할 수 있다.
- **Run**: Ralph loop의 반복(iteration) 실행 기록.
- **Artifact**: 불변 결과물(리포트, gate 로그, diff 요약, 스냅샷 등).

## 3. 상태 모델

### 3.1 Spec/Track/Task 공통 상태(권장)

- Draft: 초안
- Proposed: 제안됨(생성되었으나 승인 전)
- Approved: 승인됨
- Ready: 의존성 충족으로 실행 가능
- Running: 실행 중
- Blocked: 외부 이슈로 중단
- Done: Gate 통과로 완료
- Failed: 최대 반복 도달 등으로 실패 종료
- Archived: 불변 history로 아카이브됨
- Canceled / Superseded: 변경 요청(Change Request)으로 취소/대체됨

### 3.2 불변성 원칙

- Done/Archived로 전환된 Run/Artifact는 수정 금지
- 진행 중 또는 미시작 항목은 Change Request를 통해 재구성 가능(섹션 10)

## 4. 핵심 워크플로우

### 4.1 신규 프로젝트(그린필드)

1) 사용자가 목표 입력: “이런 SW를 만들고 싶다”
2) Interview Mode로 요구사항 수집 및 구조화
3) Spec/Track/Task(의존성 포함 Task Graph) 생성
4) 사용자 승인(Approval Gate)
5) 승인된 순서대로 진행 또는 사용자가 특정 Task를 선택해 실행
6) 완료 시 docs 업데이트 및 history 아카이브

### 4.2 기존 프로젝트(브라운필드) 전환

1) 사용자가 기존 PRD, Task 목록, 진행 문서 등을 입력
2) 문서 파싱 + 코드 분석(LSP, 검색, 테스트)으로 kkachi 포맷으로 변환
3) 문서와 코드 간 갭(누락 요구사항, 불일치, 테스트 부재)을 목록화
4) 부족분만 추가 인터뷰로 보강
5) 사용자 승인 후 실행

### 4.3 Task 실행(예: TASK-283 시작)

1) Moderator가 여러 worker들을 활용하여 Task 컨텍스트 수집(repo 범위, 관련 파일, 의존성)
2) Task Implementation Spec(작업 명세서) 작성 후 사용자에게 제시
3) 사용자 피드백 반영(질의응답)
4) 사용자 승인 후 구현 시작
5) Gate(lint/validator/test/LSP) 실행
6) 실패 시 Ralph loop에 따라 반복(최대 반복, 중단 조건 포함)
7) 성공 시 Done 처리, docs repo 업데이트, history로 스냅샷/보고서 보존

## 5. 실행 모델(Moderator, Sub-moderator, Worker Sessions)

### 5.1 세션 구성

- **Main Moderator session**: 사용자와 대화하고, fan-out/join/retrigger 및 최종 의사결정을 수행
- **Sub-moderator session**: 읽기 전용 보조 의사결정자
  - 역할: Critic(계획 비평), Arbiter(충돌 중재), 수렴 전략 제안
- **Reader workers**: 읽기 전용 조사/분석 세션(병렬)
  - 예: 코드 스캔, 호환성 점검, 문서 검색, 리스크 스캔
- **Writer worker**: 실제 변경을 적용하는 세션
  - 기본 정책: worktree 당 writer 1
- **Gate runner**: lint/validator/test/LSP diagnostics를 실행하고 결과를 정규화

### 5.2 병렬 실행 패턴(ultrawork UX)

- Fan-out: Moderator가 여러 Reader worker를 동시에 실행
- Join: kkachi가 worker 결과를 수집하여 Main Moderator session에 요약 메시지로 주입
- Retrigger: join 결과를 반영해 다음 작업(추가 조사, 구현, 수정)을 자동 진행
- 사용자 입장에서는 “Moderator 1명에게 맡기면 끝까지 수행”으로 보이도록 UI를 설계

### 5.3 Writer 충돌 방지

- 기본: writer는 단일 worktree에서 1개만 실행
- 병렬 writer가 필요한 경우: repo별 또는 task별 추가 worktree를 생성하고, 최종 병합 전 gate로 검증

## 6. Operator(초기 8개) 정의

v0.1은 아래 8개의 Operator를 기본 역할로 정의한다.

1) **Orchestrator(Moderator)**: 작업 분해, 워커 배치, 루프/게이트 총괄
2) **SpecWriter**: spec/plan 및 Task Implementation Spec 작성, 승인 흐름 지원
3) **Explorer**: LSP/검색 기반 코드 탐색, 원인 규명
4) **CompatChecker**: server-client 계약, API/타입 호환성 점검
5) **Fixer(Writer)**: 작은 변경 위주의 버그/기능 구현
6) **Refactorer(Writer)**: 구조 개선 및 큰 변경(필요 시)
7) **Gatekeeper**: lint/validator/test 실행 및 실패 분석
8) **Reviewer**: 변경 검토, 리스크/추가 TODO 제시

참고

- **Sub-moderator**는 8개 Operator의 별도 역할로 운영한다(읽기 전용).

## 7. 모델 라우팅(Agent-Model 가변 연결)

### 7.1 요구사항

- Operator별 모델을 **런타임에 변경** 가능해야 한다.
  - 예: Orchestrator를 Gemini로 쓰다가 GLM으로 변경, Codex로 변경
- OpenAI는 **GPT-5.2**와 **GPT-5.2 Codex** 2개 큰 모델을 지원하며, 각각 `xhigh/high/medium/low` 변형 선택을 지원한다.
- 사용자는 Task 실행 시 아래를 지정할 수 있다.
  - Orchestrator 모델
  - Sub-moderator 모델(Deliberation Mode)
  - worker pool 구성(타입별 개수, 최대 병렬 수)

### 7.2 구현 핵심

- kkachi가 프로젝트/Track/Task/Iteration별 **Routing Policy**를 보유한다.
- 실행 시 OpenCode 세션 프롬프트 호출에 모델 식별자를 동적으로 주입한다.
- 모델 프리셋은 `provider/model + variant` 형태로 관리하고, UI에서 선택한다.

## 8. Gate와 Ralph loop

### 8.1 Gate(품질 게이트)

프로젝트별로 다음 Gate를 정의할 수 있어야 한다.

- lint
- validator(타입체크, 스키마 검증, 문서 검증 등)
- test
- LSP diagnostics(가능한 경우)

### 8.2 Ralph loop

- 사용자가 Task 실행 요청 시 다음을 지정할 수 있어야 한다.
  - 종료 조건(until): 예 `tests=pass AND lint=pass AND validator=pass AND lsp_diagnostics=0`
  - 최대 반복 횟수(max iterations)
  - 병렬 worker 수(max workers)
  - worker 타입 및 모델 믹스(예: gemini 1, glm 1, codex 1)
- 루프는 `수정 -> Gate 실행 -> 조건 평가` 순서로 반복하고, 실패/차단 시 원인과 다음 권장 액션을 기록한다.
- 반복이 길어질 때 Sub-moderator가 수렴 전략을 제안할 수 있다.

## 9. 멀티 리포 및 worktree

### 9.1 멀티 리포 Workspace

- 하나의 Workspace는 여러 Repo를 등록할 수 있다.
  - server repo
  - client repo
  - docs repo
- Task는 repo scope를 가진다.
  - server only, client only, docs only, server+docs, server+client+docs 등

### 9.2 worktree 기반 격리

- Task 실행은 기본적으로 git worktree를 사용해 격리한다.
- 병렬 writer가 필요한 경우 worktree를 추가 생성한다.
- Running 중 피봇/중단이 발생하면 worktree 스냅샷(패치, 커밋, 로그)을 artifact로 보관할 수 있어야 한다.

## 10. Spec/Track/Task 문서 관리(문서 repo)

### 10.1 문서 원칙

- Spec/Plan은 사람이 리뷰 가능한 Markdown 중심으로 저장한다.
- 작업 중 문서(spec, plan)는 변경 가능하되, 버전화와 승인 이력을 남긴다.
- 완료 후 산출물은 history 영역으로 이동하거나, 불변 스냅샷으로 고정한다.

### 10.2 Spec/Plan 기본 산출물(권장)

- `spec.md`: 목표, 범위, 제약, 성공 조건, 리스크, 비기능 요구사항, gate 기준
- `plan.md`: phases/tasks/subtasks, 의존성, 완료 정의, 롤백/마이그레이션 전략
- `task/<task_id>.md`: Task Implementation Spec(작업 명세서)

### 10.3 Tag/추적성(선택 기능)

- 코드 변경과 Spec/Task를 연결하기 위한 Tag를 지원한다.
  - 예: 커밋 메시지 또는 코드 주석에 `@SPEC:<track_id>` / `@TASK:<task_id>`
- Gate 단계에서 태그 존재/정합성을 검증하는 옵션을 제공한다.

## 11. History(불변 이력)와 분석

### 11.1 목표

- worker 실행, gate 결과, 의사결정, diff 요약 등을 **Task 단위**로 누적 보관한다.
- 완료된 작업은 수정 불가로 고정하고, 나중에 검색 및 분석 가능해야 한다.

### 11.2 저장 전략

- **SQLite**: append-only 이벤트 로그 기반
  - Track/Task/Run/Worker 실행 기록
  - 모델/variant 사용 기록
  - gate 결과 및 반복 횟수
- **파일 아티팩트**: 사람이 읽기 좋은 보고서와 로그
  - 보고서(Markdown), gate 로그, diff 요약, 스냅샷 패치
  - 아티팩트는 content-hash 기반 참조를 권장

### 11.3 Conductor-style task 기록(채택)

- 각 worker 실행을 “Track 내 Task/Sub-task”로 식별하고, 결과를 history에 누적 저장한다.
- plan.md에는 완료된 task의 history artifact 링크를 남길 수 있어야 한다.

## 12. Tool Search(MCP 컨텍스트 절약)

### 12.1 목표

- MCP 도구 정의로 인한 컨텍스트 낭비를 줄인다.
- 필요할 때만 도구를 찾아서 사용하도록 한다(검색 -> 상세 -> 호출).

### 12.2 구현 방향(요구사항)

- kkachi는 Tool Registry를 유지하고, 다음 메타 툴을 제공할 수 있어야 한다.
  - `tool_search(query, tags, server...)`
  - `tool_info(tool_id)`
  - `tool_call(tool_id, args)`
- Tool Registry는 SQLite(FTS) 또는 파일 인덱스로 구성 가능하다.
- 필요한 경우 DCP(컨텍스트 프루닝) 같은 플러그인과 함께 운용할 수 있다.

## 13. 변경 관리(Change Management, 피봇 지원)

### 13.1 요구사항

- 설계 미스 또는 피봇 요청 시, **이미 Archived된 history는 수정하지 않되**, 아래 대상은 수정/재구성 가능해야 한다.
  - Draft/Proposed/Approved/Ready/Running 상태의 Spec/Track/Task
- 변경은 단순 편집이 아니라 **버전/대체(supersede)**로 모델링한다.

### 13.2 Change Request(CR)

- kkachi는 Change Request를 1급 엔티티로 제공한다.
- CR에는 다음이 포함되어야 한다.
  - 변경 유형(minor/major/pivot/reorder/scope-cut)
  - 변경 사유(Why)
  - 영향 범위(어떤 Spec/Track/Task, 어떤 repo)
  - 영향 분석(취소/대체/추가되는 Task, 의존성 변화)
  - 전환 계획(마이그레이션, 롤백)
  - 변경안 승인

### 13.3 Running Task 처리

- 피봇/대규모 변경 시 Running Task는 다음 옵션을 제공한다.
  - Pause + Snapshot(기본)
  - Abort + Snapshot
  - Salvage(재사용 가능한 변경을 신규 Task로 이관)

### 13.4 UI 지원(필수)

- Change Request Wizard(변경 사유, 범위, 제안안)
- Impact Report(변경 전/후 비교, 취소/대체 목록)
- Task Graph Diff(의존성 그래프 비교)
- Approval 기록 및 타임라인

## 14. Web UI 요구사항

### 14.1 화면 구성(최소)

- Workspace Dashboard
  - repo 목록, 현재 진행 중 Track/Task, 최근 gate 실패, 추천 다음 작업
- Spec/PRD View
  - spec/plan 버전, 승인 기록, 변경 요청(CR) 현황
- Track Board
  - 대기/진행/완료 칸반, 의존성, 병렬 가능 작업 묶음
- Task Detail
  - Task 명세서, 실행 로그, diff, gate 결과, worker 보고서, 히스토리 링크
- Run Timeline
  - Ralph loop iteration별 결과, 실패 원인, 수렴 전략 기록
- History/Archive
  - 완료 항목 검색, 필터(모델/기간/repo), 보고서 열람

### 14.2 UX 원칙

- 사용자는 “Moderator 1명이 끝까지 진행”하는 느낌을 유지한다.
- 병렬 worker는 내부적으로 돌아가되, UI에는 상태(시작/진행/완료)와 핵심 결과만 요약하여 표시한다.
- 승인 없는 대규모 변경은 실행되지 않도록 가드한다(옵션 정책).

## 15. 권한/안전장치

- Reader worker와 Sub-moderator는 기본적으로 **read-only**로 동작한다.
- Writer worker만 파일 편집/커밋 권한을 가진다.
- docs repo 수정 권한은 원칙적으로 문서 업데이트 Task(scope=docs)에만 부여한다.
- 외부 디렉토리 접근은 기본 차단 또는 ask 정책으로 운영하며, Workspace에 등록된 repo만 기본 allow로 취급한다.

## 16. v0.1 구현 범위(MVP)

### 16.1 포함

- Web UI: Workspace/Track/Task 목록, Task 실행, 상태/로그/게이트 표시
- Interview Mode: 신규 프로젝트 Spec/Track/Task 생성 및 승인
- Import Mode: 기존 PRD/Task 문서 입력 -> kkachi 포맷 변환 -> 갭 탐지 -> 승인
- Moderator + Sub-moderator + worker session 런타임
- Gate + Ralph loop (사용자 지정 조건, 최대 반복, 병렬 워커, 워커 타입 선택)
- worktree 기반 실행 격리
- SQLite 기반 history(append-only) + docs repo 아티팩트 저장
- Change Request 기반 피봇 지원(버전/대체, 영향 분석, 재승인)
- Tool Search 기본 기능(검색 -> 상세 -> 호출)과 Tool Registry 기초

### 16.2 제외 또는 옵션

- 고급 병렬 writer(자동 병합/충돌 해결) 완전화
- 고급 자동 릴리즈/배포 파이프라인
- 외부 시스템(Jira, Linear 등)과의 양방향 동기화

## 17. Open Questions(추가 답변이 필요한 질문 목록)

아래는 대화 과정에서 제가 드린 질문 중, 아직 설계 확정을 위해 답변이 필요한 항목만 추린 목록이다.

1) **MoAI 계열 기능의 MVP 포함 범위**

- SPEC/Plan-Run-Sync, Tag 추적성, worktree 운영 명령군을 v0.1에 어디까지 포함할지

1) **Ralph loop 종료 조건(기본값) 정의**

- 기본 until 조합을 `tests`, `lint`, `validator`, `lsp_diagnostics` 중 무엇으로 둘지
- 실패 시 정책(즉시 중단, 보고서만, 재승인 후 재시도 등)

1) **Spec/Track 파일 배치 위치(문서 repo 구조)**

- `conductor/` 호환 구조로 둘지, kkachi 고유 디렉토리(`.kkachi/`, `.opencode/` 등)로 둘지

1) **Gate 기본값 및 프로젝트별 설정 방식**

- 언어별 권장 프리셋(JS/TS, Go 등)을 제공할지
- 아니면 프로젝트별 커맨드 지정 방식만 지원할지

1) **Task ID 체계**

- 기존 `TASK-283` 같은 ID를 그대로 1급으로 유지할지
- `KK-YYYY-NNNN` 같은 kkachi ID를 새로 만들고 매핑할지

1) **Approval Gate 단계 수**

- Spec 베이스라인 승인만 할지
- Task Implementation Spec 승인까지 필수로 둘지
- Merge/Release 전 승인까지 둘지(선택)

1) **병렬 writer 허용 범위(기본 정책)**

- v0.1에서 writer는 worktree 당 1개로 고정할지
- 특정 조건에서만 sub-worktree로 writer 병렬을 허용할지

1) **Tool Search 구현 선택**

- v0.1에서 kkachi가 Tool Registry를 직접 구축할지
- 또는 기존 오픈소스 플러그인을 채택하고 kkachi가 UX/설정만 관리할지

1) **초기 플러그인 번들(선택 사항)**

- DCP(컨텍스트 프루닝), PTY, shell-strategy, notifier, supermemory, morph-fast-apply, websearch-cited 등을 v0.1 기본 번들에 포함할지


## 추가 및 확인 필요 사항

1) 동일 worker를 병렬로 여러개 생성 가능한가?

2) Task 설계 요청. 애매하거나 사용자의 결정이 필요한 부분이 있었는지 인터뷰. 구현 작업 진행. 충분한 테스트(happy & 중요 edge case) 추가. 코드 중복 및 최적화 작업. 문서 업데이트.

3) 하나의 workspace에서 둘 이상의 작업을 동시에 할 수 있나? 하나는 TASK-132, 다른 하나는 TASK-198 이렇게

4) oh-my-claude-sisyphus와 oh-my-opencode를 참고해서 추가할 agent들은 없을까?

5) moderator, sub-moderator, worker 들의 AI model 변경은 어떻게 하지? 한번 세션이 시작하면 변경을 할 수 없을까?

## References

- <https://github.com/modu-ai/moai-adk?tab=readme-ov-file>
- <https://github.com/Yeachan-Heo/oh-my-claude-sisyphus>
- <https://github.com/code-yeongyu/oh-my-opencode>
- <https://github.com/gemini-cli-extensions/conductor>
