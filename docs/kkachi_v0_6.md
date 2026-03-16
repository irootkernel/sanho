# kkachi 요구사항 정리 - v0.6

## 0. 확정된 합의/전제

### 0.1 제품 정체성

- **kkachi = Web-first Control Plane**
  - OpenCode는 **Execution Plane(실행 엔진)** 역할
  - kkachi는 **Orchestration/History/Workspace/Gate/Loop/Approval/UX**를 제공

### 0.2 병렬성에 대한 핵심 결론

- 사용자가 원하는 UX는 “**Moderator 1명에게 맡기면, 알아서 여러 agent를 병렬 운용하고(join/retrigger) Ralph loop로 끝까지 완료**”이다.
- 다만 실행 메커니즘 관점에서 병렬 단위는 **(서브에이전트가 아니라) worker session**이 안정적이다.
- 따라서 kkachi는:
  - **Moderator session 1개**는 유지(사용자 UX의 중심)
  - 병렬 실행은 **worker session pool**로 fan-out
  - 완료 결과를 **message injection**으로 메인 세션에 되돌림

### 0.3 실행 모델 핵심: Manager

- Manager는 “보조(assistant)”가 아니라 **감독/승인/품질**을 담당하는 **거버넌스 주체**다.
  - 감독: Moderator의 작업 분해/위임이 합리적인지 검토하고 필요 시 재지시
  - 승인 준비: 사용자가 승인할 수 있도록 근거와 리스크를 정리한 **Approval Packet** 생성
  - 품질: 설계/구현/검증 결과가 Gate 통과만으로 충분한지 추가 점검(회귀, 엣지, 문서 반영, 리스크)
- 기본 원칙: Manager는 “최종 변경을 직접 적용”하지 않는다.
  - 코드/문서 편집은 Worker(Writer)로 위임하고, Manager는 리뷰 및 승인 준비 중심으로 운영한다.

### 0.4 3-티어(Moderator/Manager/Worker)와 Role(Operator)의 분리

- **Actor Class(권한과 책임)**
  - **Moderator**: 오케스트레이션, 중계(교환수), 상태 관리
  - **Manager**: 감독, 승인, 품질
  - **Worker**: 실행(조사/작성/검증)
- **Role(Operator, 목적 중심 라벨)**
  - orchestrator, architect, specwriter, explorer, writer, gatekeeper, reviewer 등
- 한 실행 단위(worker_job)는 아래 메타를 가진다.
  - `{actor_class, role, permissions, model_profile}`

### 0.5 Manager 분화(선택, 단계별 감독)

- Greenfield 설계 감독(Architect Manager)
- Brownfield(import/normalize) 설계 감독(Architect Manager)
- Task 명세(TIS) 감독(Task Spec Manager)
- 구현 결과 감독(Implementation Manager)
- 테스트/검증 감독(Verification Manager)

> MVP에서는 Manager를 1개 세션으로 두고 role을 스위칭하는 형태로 시작하고, 필요 시 단계별 Manager로 분화한다.

### 0.6 토큰 효율을 위한 모델 티어링

- 목표는 “좋은 SW를 만들면서, 토큰을 효율적으로 배분”하는 것이다.
- 권장 기본 매핑(원칙)
  - **Manager는 고급 모델(high)**: 설계/리뷰/승인 패킷 작성은 정보량과 판단이 중요
  - **Moderator는 중급 모델(medium)**: 오케스트레이션과 조율은 상대적으로 저비용으로도 가능
  - **Worker는 역할에 따라 low~high를 혼합**
    - Reader/Gate: low 또는 medium
    - Writer: medium(복잡도 상승 시 high로 승격)
- Manager 개입은 다음에 집중한다.
  - Spec/Task/Merge/Release 승인 체크포인트
  - 반복 실패(maxIterations로 수렴이 안 되는 상황)
  - 워커 결과 충돌(conflict) 발생

### 0.7 확정 정책

- **문서 repo 구조**: `conductor/` 호환보다 **kkachi 고유 구조** 채택
- **Gate 기본값**: MVP는 **프로젝트 커맨드 기반**만 먼저 지원하고, 언어별 프리셋은 이후 추가
- **Task ID**: `TASK-283` 같은 **기존 형식 유지**
- **Approval Gate 단계**: Spec/Task/Merge/Release는 모두 승인 게이트로 관리한다. (정책 모드에 따라 사용자 승인 또는 자동 승인)
- **Writer 병렬 정책**: **worktree 당 writer 1 고정**(기본)
- **Tool Search**: MVP부터 **SQLite 기반 Tool Registry를 kkachi에서 직접 구현**(플러그인은 보조 수단)
- **MVP 기본 플러그인 번들**: DCP/PTY/shell-strategy/websearch-cited/morph-fast-apply/notifier(or notificator)/supermemory/skillful 등 **모두 포함**
- **멀티 repo 실행 기본값**: **repo/worktree별 OpenCode 백엔드 분리(옵션 C)**
- **Change Management**: Running task가 있을 때 재설계/피봇이 발생하면 **계속 진행 vs abort는 사용자가 결정**

---

### 0.8 Approval 정책 모드(Strict Approval vs All Allow)

kkachi는 모든 승인 지점을 **승인 게이트(Approval Gate)** 로 모델링하되, 실행 정책을 두 가지 모드로 제공한다.

- **Strict Approval 모드(기본값)**
  - Spec/Task/Merge/Release/CR 등 승인 게이트에서 **항상 사용자 승인을 요청**한다.
  - Interview(질문-응답)도 기본적으로 **차단형(blocking)** 으로 진행한다.
  - 목적: 안전성, 감사 가능성, 의사결정 품질.

- **All Allow 모드(옵션, autopilot)**
  - Spec/Task/Merge/Release 승인 게이트를 **자동 승인**하고, 설계부터 구현/검증까지 한 번에 진행한다.
  - 단, 자동 승인은 “승인 이벤트”로 history에 기록되며(approved_by = system), Approval Packet/근거(artifact_ref)는 동일하게 생성한다.
  - Interview는 원칙적으로 차단형 진행이 어려우므로, **가정 기반(assumption-driven) 인터뷰**로 대체한다.
    - kkachi/Manager가 선택지와 권장안을 제시하고, 사용자가 즉시 답하지 않더라도 **권장안으로 진행**한다.
    - 모든 가정과 Decision Points는 문서와 history에 남기며, 필요 시 CR(Change Request)로 되돌릴 수 있게 한다.
  - 목적: 속도(원샷), 반복적인 저위험 작업 자동화, 데모/프로토타이핑.

모드 적용 범위(권장)

- Workspace 기본값 + Task 실행 시 override 둘 다 지원
- UI(Approval Center)에서 현재 모드를 항상 노출

#### 0.8.1 Approval 모드와 실행 모드(aao)의 관계

Approval(승인) 모드와 병렬 실행 모드(allatonce/aao)는 **별개 축**이다. 일반적으로 아래 조합을 지원한다.

| 구분 | 옵션 | 의미 | 권장 사용처 |
|---|---|---|---|
| Approval Mode | strict | 사용자 승인 필수(게이트마다) | 운영/프로덕션 변경, 리스크 높은 작업 |
| Approval Mode | all_allow | 자동 승인(단, Auto-Pause 정책 적용) | 프로토타이핑, 반복 작업, 데모 |
| Execution Mode | normal | 필요한 최소 worker만 사용 | 비용 절감, 단순 작업 |
| Execution Mode | aao | reader 병렬을 과할 정도로 극대화 | 원인 규명/탐색, 복잡한 레거시 분석 |

권장 기본값

- Workspace 기본: `approval_mode=strict`, `execution_mode=normal`
- Task 실행 override: `approval_mode=all_allow` 또는 `execution_mode=aao`를 선택 가능

### 0.9 Interview 정책(선택지, 장단점, 권장안 제공)

설계 단계의 Interview는 다음 원칙을 따른다.

- 질문을 할 때는 항상 **선택지**를 제공한다.
- 각 선택지에 대해 **장단점**을 명시한다.
- 가능한 경우 **권장안(recommended)** 을 제시한다.
- All Allow 모드에서는 답변을 기다리지 않고 권장안으로 진행하되, 해당 결정을 **Decision Points** 로 기록한다.

#### 0.9.1 Assumption Bank / Decision Points(결정 로그) 저장

Interview에서 나온 “결정”은 대화 내에서 휘발되지 않도록, docs repo와 DB 이벤트 로그에 동시에 기록한다.

- docs repo(정본) 저장 위치(권장)
  - `kkachi/tracks/<TRACK_ID>/decisions/decisions.jsonl`
  - `kkachi/tracks/<TRACK_ID>/assumptions/assumptions.md` (사람 친화)
- DB(events) 저장
  - `events.kind = decision.recorded` 로 append-only 기록
  - UI는 events를 projection하여 “Decision Points 큐”를 제공

Decision Point 레코드(예시)

```yaml
decision_point:
  id: DP-0007
  topic: "Auth 방식"
  question: "사용자 인증은 Firebase Auth로 고정할까?"
  options:
    - id: A
      label: "Firebase Auth"
      pros: ["구현/운영 단순", "클라이언트 SDK 생태계"]
      cons: ["벤더 종속", "프로젝트 정책 제약"]
    - id: B
      label: "GitHub OAuth"
      pros: ["개발자 친화", "권한/조직 연동 용이"]
      cons: ["일반 사용자 서비스에는 부적합할 수 있음"]
  recommended: A
  chosen: A
  chosen_by: system   # strict에서는 user
  status: pending_user_confirm   # strict에서는 confirmed
  reversibility: "medium"
  impact: "auth-subsystem"
  evidence_refs:
    - "artifact:sha256:..."
```

All Allow 모드에서의 규칙

- chosen_by = system, status = pending_user_confirm로 기록
- 사용자가 이후 다른 선택을 원하면 CR로 전환(15장) 후 재실행/재정렬

### 0.10 All Allow 모드 Auto-Pause 승격 정책(상세)

All Allow 모드는 “기본은 자동 진행”이지만, 특정 신호가 감지되면 **자동으로 Pause**하고 사용자 확인(또는 strict 전환)을 요구한다. 이는 “무조건 자동”이 아니라 **자동 + 안전장치**를 목표로 한다.

Auto-Pause 평가 시점(권장)

- (A) 설계 완료 후(TIS 생성 직후)
- (B) 첫 writer 변경 적용 직후(첫 diff 생성 시)
- (C) 매 gate 실행 결과 수집 직후
- (D) merge 직전(merge readiness)
- (E) release 직전(release readiness)

#### 0.10.1 감지 신호와 기본 임계값(권장)

| 구분 | 감지 신호(예시) | 기본 임계값(예시) | Auto-Pause 액션 | 비고 |
|---|---|---:|---|---|
| 보안/권한 | auth/permission/crypto 관련 경로 변경 또는 권한 스코프 확대 | path match | Pause + Manager(Security) 리뷰 강제 | Workspace에서 패턴 커스터마이즈 |
| 시크릿/민감정보 | secret scan(예: 키 패턴) 또는 .env/credential 파일 변경 | any | 즉시 Pause + 변경 되돌리기 권장 | hard |
| 데이터 마이그레이션 | migration 파일 생성/수정, schema 변경 감지 | any | Pause + 사용자 확인(다운타임/롤백) | hard |
| 결제/과금 | billing/payment 관련 모듈 변경 | path match | Pause + 사용자 확인 | hard |
| 의존성 변경 | go.mod/package.json/lockfile 변경 | any | Pause(soft) + Manager 리뷰 후 진행 가능 | false positive 가능 |
| 대규모 변경량 | changed_files > 30 | 30 | Pause(soft) + scope 재검토 | 기본값 조정 |
| 대규모 변경량 | insertions+deletions > 1000 | 1000 | Pause(soft) | |
| 대규모 삭제 | deletions_ratio > 0.35 | 0.35 | Pause + 사용자 확인 | hard |
| 반복 실패 | gate_fail_streak >= 3 | 3 | Pause + stabilization plan 생성 | v0.5 정책 구체화 |
| 충돌 | worker 결론 상충, merge conflict | any | Pause + Manager(on_conflict) | |
| 파괴적 커맨드 | rm -rf, drop table 등 위험 커맨드 감지 | any | 즉시 Pause | shell-strategy 기반 감지 |

- hard: 사용자 확인 없이는 진행 금지(Workspace에서 완화 가능)
- soft: 기본은 Pause지만, 사용자가 “계속”을 선택하거나 Manager가 안전하다고 판정하면 진행 가능

#### 0.10.2 Auto-Pause 발생 시 사용자 선택지(표준)

Auto-Pause가 발생하면 kkachi는 **Pause Packet**을 생성하고 Approval Center에 노출한다. 사용자는 아래 중 하나를 선택한다.

1) Continue (All Allow 유지)
2) Continue but switch to Strict Approval (이 Task 또는 Workspace)
3) Abort (Snapshot 유지)
4) Create CR and re-plan (피봇/스코프 변경)

#### 0.10.3 Pause Packet 포맷(권장)

Pause Packet은 Approval Packet과 같은 규격으로 관리하되, trigger와 권장 액션을 포함한다.

```yaml
pause_packet:
  entity: TASK-283
  phase: "merge_ready"
  triggered_by:
    - signal: "large_deletion_ratio"
      value: 0.48
      threshold: 0.35
    - signal: "sensitive_path_change"
      value: "server/auth/*"
  summary: "삭제 비율이 높고 auth 경로 변경이 포함되어 자동 Pause"
  risks:
    - "인증 흐름 regress 가능"
    - "기존 세션/토큰 호환성 문제"
  recommended_action: "switch_to_strict"
  evidence_refs:
    - "artifact:sha256:... (diff summary)"
    - "artifact:sha256:... (gate logs)"
```

## 1. 목적 및 범위

**kkachi(까치)**는 OpenCode를 실행 엔진(Execution Plane)으로 사용하고, 그 위에 Web 기반 컨트롤 플레인(Control Plane)을 구축하는 제품이다.

핵심 목표:

- 사용자의 목표를 인터뷰로 구조화해 **Spec, Track, Task**로 설계하고, **승인(Approval)**을 받은 뒤 구현을 진행
- 멀티 리포(server, client, docs)를 하나의 Workspace로 묶어, 코드/문서/작업 이력을 함께 운영
- **Moderator 중심 작업**을 기본으로 하되, 다수의 **worker session**을 병렬 운용(fan-out, join, retrigger)하여 조사/검증/구현 효율을 높임
- **Manager**가 설계/구현/검증의 체크포인트에서 감독 및 승인 준비를 수행해 Ralph loop 반복 횟수를 줄임
- lint, validator, test, LSP diagnostics 기반의 **Gate**와 **Ralph loop**를 결합해 “끝까지 완료”를 자동화
- 완료된 결과물과 로그는 **불변(immutable) history**로 누적하고, SQLite 기반으로 검색/분석 가능하게 함

비목표(Non-goals)

- `oh-my-opencode` 번들(에이전트/훅/스킬/설정)을 가져다 쓰지 않는다

---

## 2. 아키텍처(2-plane): Control Plane + Execution Plane

### 2.1 High-level 컴포넌트

- **kkachi Web UI**: Workspace/Spec/Track/Task, 승인, 실행, 로그/리포트 확인
- **kkachi API Server**: 오케스트레이션 런타임(스케줄러/루프 엔진), DB, 파일 아티팩트 관리
- **SQLite**: append-only 이벤트 로그 + 상태 프로젝션
- **Git/Worktree Manager**: repo/worktree 생성/정리/스냅샷/병합
- **OpenCode Server(s)**: 세션 생성/프롬프트 실행/툴 실행/LSP 포함 코드 실행 엔진
- **OpenCode Plugins (MVP 기본 번들 포함)**: DCP, PTY, shell-strategy, websearch-cited 등

### 2.2 PlantUML: High-level

```plantuml
@startuml
title kkachi high-level architecture (Web Control Plane over OpenCode)
skinparam componentStyle rectangle

actor "User" as U

component "kkachi Web UI" as Web
component "kkachi API Server\n(Orchestrator Runtime)" as K

database "SQLite\n(append-only history)" as DB
component "Git/Worktree Manager" as WT
component "Artifact Store\n(files + content hash)" as AS

node "OpenCode Execution Plane" as OCEP {
  component "OpenCode Server(s)" as OCS
  component "LSP Servers" as LSP
  component "OpenCode Plugins\n(DCP/PTY/shell-strategy/...)" as PL
}

U --> Web
Web --> K
K --> DB
K --> WT
K --> AS
K --> OCS : create sessions / prompt_async / message injection
OCS --> LSP
PL --> OCS
@enduml
```

---

## 3. 핵심 개념(엔티티) 및 저장소 모델

### 3.1 엔티티

- **Workspace**: 여러 Repo를 묶는 작업 단위. 사용자/권한, 모델 라우팅, Gate, 정책 포함
- **Repo**: server, client, docs 등 개별 저장소
- **Spec**: 요구사항 및 성공 조건, 제약, 리스크를 담는 최상위 문서
- **Track**: Spec을 구현하기 위한 큰 작업 흐름(에픽 수준). Task 그래프(DAG) 포함
- **Task**: 실행 가능한 최소 작업 단위(`TASK-283` 등). repo scope, gate 조건, loop 조건 포함 가능
- **Task Dependency Edge**: Task 간 선후 관계(의존성)
- **Run**: Ralph loop의 iteration 실행 기록
- **Worker Job**: reader/writer/gate/manager 작업 실행 단위
- **Artifact**: 불변 결과물(리포트, gate 로그, diff 요약, 스냅샷 등)
- **Change Request(CR)**: 피봇/대규모 변경을 모델링하는 1급 엔티티
- **Approval**: Spec/Task/Merge/Release/CR 등에 대한 사용자 승인 이벤트

### 3.2 저장 전략(2-layer)

- **Mutable layer(진행 상태)**
  - spec/plan/Task Implementation Spec 문서는 작업 중 수정 가능
  - 단, 버전과 승인 이벤트를 남김
- **Immutable layer(증거/결과물)**
  - worker 보고서, gate 로그, diff 요약, 스냅샷 등은 **append-only**

### 3.3 DB 스키마

> 목적: “무엇을/언제/어떤 모델로/어떤 결과로/어떤 변경을 했는지” 추적. 완료된 기록은 수정 불가.

핵심 테이블(예시)

- workspaces(id, name, created_at, settings_json)
- repos(id, workspace_id, kind(server|client|docs), origin_url, local_path, default_branch, created_at)
- users(id, email, display_name, created_at, last_login_at)
- workspace_members(workspace_id, user_id, role(owner|maintainer|writer|reader|auditor), created_at)

- specs(id, workspace_id, title, active_version, created_at)
- spec_versions(spec_id, version, content_hash, created_at, supersedes_version, active_flag)

- tracks(id, workspace_id, spec_id, title, status, active_version, created_at, closed_at)
- track_versions(track_id, version, spec_version, created_at)

- tasks(id, track_id, external_id, title, status, active_version, scope_json, created_at, closed_at)
- task_versions(task_id, version, track_version, spec_hash, loop_spec_json, gate_spec_json, created_at, supersedes_task_id, canceled_reason)
- task_deps(task_id, depends_on_task_id, created_at)

- runs(id, task_id, iteration, started_at, ended_at, outcome, summary_hash)
- worker_jobs(id, run_id, kind(read|write|gate|manage), operator, model_ref, repo_id, worktree_path, status, started_at, ended_at, report_hash)

- artifacts(hash, kind, path, size_bytes, created_at, meta_json)
- approval_requests(id, workspace_id, entity_type, entity_id, version, checkpoint, repo_id, requested_by_actor, requested_at, approval_packet_ref, status, resolved_event_id)
- approvals(id, entity_type(spec|task|merge|release|cr), entity_id, version, checkpoint, repo_id, status, mode_at_decision, approved_by_type(user|system), approved_by_user_id, approved_at, approval_packet_ref, note)
- decision_points(id, workspace_id, track_id, task_id, topic, question, options_json, recommended, chosen, chosen_by_type(user|system), status, evidence_refs_json, created_at)

- change_requests(id, workspace_id, type, reason, status, created_at, approved_at)
- events(id, ts, entity_type, entity_id, kind, payload_json, sha256)

불변성 강제(예)

- events/artifacts/history 영역은 UPDATE/DELETE 금지 트리거
- Done/Archived로 전환된 run/task_version 레코드는 변경 금지(새 버전만 생성)

---

### 3.4 Canonical State Packet(세션 컨텍스트 재구성 표준)

OpenCode 세션은 provider/모델에 따라 컨텍스트 유지 방식이 달라질 수 있다. 특히 **서로 다른 provider로 전환**하는 경우(예: glm -> gemini)에는 “같은 세션에서의 연속 컨텍스트”를 기대하기 어렵다.

따라서 kkachi는 DB의 상태 프로젝션과 핵심 artifact를 기반으로, 언제든 세션을 재생성/재시드할 수 있는 **Canonical State Packet(CSP)** 을 생성한다.

**CSP에 포함되는 최소 정보(권장)**

- workspace_id, active_track_id, active_task_id
- 승인 상태(승인된 spec/task/merge/release 버전)와 현재 게이트 상태
- 현재 iteration, 최근 실패 원인 Top-N 요약
- 적용 중인 정책(Approval 모드, parallel.maxWorkers, 모델 라우팅 프리셋, 권한 제약)
- 핵심 artifact_ref(리포트, gate 로그, diff 요약, 스냅샷, 웹서치 근거 등)
- 미해결 질문(open_questions) 및 decision_points

**사용처**

- provider/모델 전환 시: 새 세션 생성 후 CSP를 주입하여 컨텍스트를 재구성
- worker session 생성 시: Reader/Writer/Gatekeeper가 동일한 기준 상태를 공유
- 장애 복구/재현: 특정 run/iteration을 CSP + artifact로 재현 가능

**주입 방식(예)**

- OpenCode 세션 system prompt에 CSP 요약을 포함
- 상세 CSP는 docs repo 또는 artifact store에 파일로 저장하고, system prompt에는 링크(artifact_ref)만 포함

### 3.5 Governance Event Model: Approval / Decision / Auto-Pause (상세)

kkachi는 모든 거버넌스 이벤트를 `events`에 **append-only**로 기록한다. `approvals`, `approval_requests`, `decision_points` 등은 `events`에서 파생(projection)된 테이블로 본다.

#### 3.5.1 events 테이블 권장 필드(확장)

기존 예시: `events(id, ts, entity_type, entity_id, kind, payload_json, sha256)`에 더해, 아래 필드를 권장한다.

- `trace_id`: 하나의 실행 흐름(run/iteration/approval)을 연결하는 상관관계 ID
- `entity_version`: 승인 대상 버전(spec.v2, tis.v1 등)
- `severity`: info|warn|critical (Auto-Pause triage용)

권장 형태(예시)

- events(id, ts, trace_id, entity_type, entity_id, entity_version, kind, severity, payload_json, sha256)

#### 3.5.2 Approval 이벤트 종류

| kind | 의미 | Strict Approval | All Allow |
|---|---|---|---|
| approval.requested | 승인 요청 생성(대기열에 등장) | 생성됨 | 생성됨(즉시 auto-resolve 가능) |
| approval.decided | 승인/거절 결정 | user가 결정 | system(auto) 또는 Auto-Pause 시 user |
| approval.revoked | 승인 철회(선택) | 가능 | 가능 |
| approval.superseded | 버전 교체로 기존 승인 무효(선택) | 가능 | 가능 |
| approval.mode_changed | approval_mode 변경(Workspace/Task) | 가능 | 가능 |

#### 3.5.3 Approval 이벤트 payload 스키마(예시)

`approved_by=system`을 명시적으로 표현하기 위해 `approved_by.type`을 도입한다.

**(1) approval.requested**

```json
{
  "checkpoint": "task.tis",
  "approval_mode": "strict",
  "entity": {"type": "task", "id": "TASK-283", "version": "tis.v1"},
  "repo_id": "server",
  "requested_by": {
    "type": "actor",
    "actor_class": "Moderator",
    "operator": "Orchestrator",
    "session_id": "sess_..."
  },
  "approval_packet_ref": "artifact:sha256:...",
  "context": {"run_id": "RUN-0007", "iteration": 1}
}
```

**(2) approval.decided (사용자 승인)**

```json
{
  "checkpoint": "task.tis",
  "approval_mode": "strict",
  "decision": "approved",
  "approved_by": {"type": "user", "user_id": "USR-001", "workspace_role": "owner"},
  "approval_packet_ref": "artifact:sha256:...",
  "note": "OK",
  "context": {"run_id": "RUN-0007", "iteration": 1}
}
```

**(3) approval.decided (자동 승인: approved_by=system)**

```json
{
  "checkpoint": "task.tis",
  "approval_mode": "all_allow",
  "decision": "approved",
  "approved_by": {"type": "system"},
  "approval_packet_ref": "artifact:sha256:...",
  "autopilot": {"reason": "all_allow_mode"},
  "context": {"run_id": "RUN-0007", "iteration": 1}
}
```

#### 3.5.4 Auto-Pause 이벤트 payload 스키마(예시)

```json
{
  "run_id": "RUN-0007",
  "task_id": "TASK-283",
  "iteration": 2,
  "approval_mode": "all_allow",
  "triggered_signals": [
    {"signal": "large_deletion_ratio", "value": 0.48, "threshold": 0.35, "severity": "critical"},
    {"signal": "sensitive_path_change", "value": "server/auth/*", "severity": "critical"}
  ],
  "pause_packet_ref": "artifact:sha256:...",
  "next_actions": ["continue", "switch_to_strict", "abort", "create_cr"]
}
```

#### 3.5.5 Decision Point 이벤트 payload 스키마(예시)

```json
{
  "decision_point_id": "DP-0007",
  "topic": "Auth 방식",
  "question": "사용자 인증은 Firebase Auth로 고정할까?",
  "recommended": "A",
  "chosen": "A",
  "chosen_by": {"type": "system"},
  "status": "pending_user_confirm",
  "evidence_refs": ["artifact:sha256:..."]
}
```

## 4. 실행 모델: Moderator + Manager + Worker

### 4.1 Actor Class(권한과 책임) 정의

| Actor Class | 핵심 목적 | 기본 권한(권장) | 대표 산출물 | 비고 |
|---|---|---|---|---|
| **Moderator** | 오케스트레이션과 중계(교환수) | read-only + 오케스트레이션 툴 호출 | 실행 계획, 워커 배치, 진행 요약 | 사용자 UX의 “단일 대화 상대”를 유지 |
| **Manager** | 감독, 승인 준비, 품질 보증 | read-only(코드/문서 직접 변경 금지) | Approval Packet, 리뷰 리포트, 리스크 목록 | 단계별 Manager로 분화 가능 |
| **Worker** | 실행(조사/작성/검증) | role 기반(read/write) | 조사 리포트, 패치/커밋, 게이트 로그 | 병렬 pool 운용 |

권한 원칙(기본값)

- **변경 적용은 Worker(Writer)만 수행**한다.
  - 코드 repo: Fixer/Refactorer 등 writer worker
  - docs repo: docs-scope writer worker
- **Moderator/Manager는 기본적으로 write 없는 세션**으로 운영한다.
  - 목적: 충돌/오염 방지, 감사(audit) 단순화, 책임 분리
- 필요 시 예외를 둘 수 있으나, MVP에서는 “권한 분리”가 운영 안정성에 유리하다.

### 4.2 Role(Operator)와 Actor Class의 매핑

- Role은 “무엇을 하느냐”, Actor Class는 “어떤 책임과 권한을 가지느냐”를 뜻한다.
- MVP 권장 매핑(기본값)

| Role(Operator) | 권장 Actor Class | 권장 모델 티어 | 주요 업무 |
|---|---|---|---|
| Orchestrator | Moderator | medium | fan-out/join/retrigger, 루프/게이트 제어, 승인 요청 라우팅 |
| Architect (Greenfield/Brownfield) | Manager | high | 전체 설계 방향성, 리스크/일관성 점검, 결정 로그 작성 |
| Reviewer / QA | Manager | high | 변경 수용 여부 판단, 테스트/문서/회귀 점검, 승인 패킷 생성 |
| SpecWriter | Worker(Writer, docs-scope) | medium~high | spec/plan/TIS 문서 적용(파일 반영) |
| Librarian (Read) | Worker(Reader, docs-scope) | low~medium | docs repo/kkachi history 조회/검색, 근거 수집, 참조 링크 생성 |
| LibrarianWriter (Write) | Worker(Writer, docs-scope) | medium | ADR/architecture/user scenario 및 kkachi 문서 반영(요청된 경우) |
| Explorer | Worker(Reader) | low~medium | 코드/문서 탐색, 원인 규명, 영향 범위 추정 |
| CompatChecker | Worker(Reader) | low~medium | server-client 계약/API/타입 호환성 점검 |
| Fixer(Writer) | Worker(Writer) | medium | 작은 변경 위주의 버그/기능 구현 |
| Refactorer(Writer) | Worker(Writer) | medium~high | 구조 개선, 큰 변경(필요 시) |
| Gatekeeper | Worker(Gate) | low~medium | lint/validator/test/LSP 실행 및 결과 정규화 |

### 4.3 Manager 분화(선택)와 개입 범위

Manager는 “항상 한 명”일 필요는 없다. 다만 MVP에서는 복잡도를 낮추기 위해 **Manager 1개 세션을 기본**으로 하고, 역할을 스위칭하는 형태를 권장한다.

- (확장) 단계별 Manager 예시
  - **Greenfield Manager**: 신규 Spec/Track/Task 설계 감독
  - **Brownfield Manager**: import/normalize/갭 분석 감독
  - **Task Spec Manager**: TIS(Task Implementation Spec) 완성도/누락 점검
  - **Implementation Manager**: 구현 범위 통제, 리스크/회귀 관점의 리뷰
  - **Verification Manager**: Gate, 테스트 설계, 릴리즈 준비 감독

Manager 개입 정책(권장)

- **항상 개입(Approval Checkpoint)**
  - Spec 승인 전
  - Task(TIS) 승인 전
  - Merge 승인 전
  - Release 승인 전
- **조건부 개입(Escalation)**
  - Gate 실패가 연속 N회 발생(N은 기본 3 권장)
  - 워커 결과가 상충(conflict)하거나, 결론이 분산될 때
  - 변경 범위가 TIS 대비 확대될 때(scope creep)
  - 보안/권한/데이터 마이그레이션 등 고위험 변경
- **불개입(기본)**
  - 단순 lint 수정, 단발성 테스트 실패 수정 등 저위험 반복 작업

### 4.4 Artifact 교환과 Worker 간 정보 공유

MVP 모델에서 Moderator는 “중계”가 핵심이므로, **워커 결과물(파일)을 다른 워커에게 알려주는 메커니즘**이 중요하다.

- Worker는 결과를 아래 형태로 남긴다.
  - 리포트: `history/run_0007/worker_read_0003.md`
  - 패치: `history/run_0007/patch.diff` 또는 worktree 커밋
  - 게이트 로그: `history/run_0007/gate_test.log`
- kkachi는 결과물을 content-hash로 등록하고, `artifact_ref`로 참조한다.
- Moderator는 join 시 다음을 수행한다.
  - (1) 워커 출력 요약
  - (2) 핵심 artifact_ref 목록
  - (3) 다음 워커에게 전달할 “질문” 혹은 “검증 포인트”
- Manager 리뷰는 artifact_ref(근거)를 입력으로 받아, 승인 패킷을 생성한다.

### 4.5 allatonce(aao) UX를 위한 핵심 메커니즘(유지)

- 사용자 UX는 “Moderator 1명에게 맡기면 끝”으로 고정한다.
- 내부적으로 worker, manager 세션이 병렬 실행된다.
- kkachi가 join 결과를 **message injection**으로 Moderator 세션에 주입해 “한 Moderator가 알아서 한 것처럼” 보이게 한다.

### 4.6 kkachi가 OpenCode에 제공해야 하는 커스텀 툴(최소)

- 실행/병렬
  - `kkachi.spawn_workers(...)`
  - `kkachi.join_workers(...)`
- 품질
  - `kkachi.run_gate(...)`
- 격리
  - `kkachi.worktree_new(...)`
  - `kkachi.worktree_snapshot(...)`
  - `kkachi.worktree_merge(...)`
- 거버넌스(추가)
  - `kkachi.request_manager_review(entity, artifact_refs, questions)`
  - `kkachi.request_user_approval(entity, approval_packet_ref)`

### 4.7 PlantUML: Task 실행 + Manager 리뷰 + 승인(개념)

```plantuml
@startuml
title Task execution with Manager checkpoints (concept)

actor User
participant "Moderator Session (OpenCode)" as M
participant "kkachi Runtime (Scheduler + Loop Engine)" as K
participant "Worker Sessions (Read/Write/Gate)" as W
participant "Manager Session (Governance)" as R

User -> M: Start TASK-283

M -> K: spawn readers (context scan)
K -> W: prompt_async (parallel)
K -> M: join + message injection (summaries + artifact_refs)

M -> K: request manager review (TIS readiness)
K -> R: prompt_async (review based on artifact_refs)
K -> M: message injection (approval_packet_ref)

M -> K: request USER approval (Task/TIS)
K -> User: approval UI (Task)
User -> K: Approve

M -> K: spawn writer (implement)
K -> W: prompt_async (writer)
M -> K: run_gate
K -> W: prompt_async (gatekeeper)
K -> M: gate results + logs

alt gate fail
  M -> K: spawn fixer (small fix)  
  K -> W: prompt_async (writer)
  M -> K: run_gate
  K -> M: gate results
  opt repeated failures or risky
    M -> K: request manager review (stabilization strategy)
    K -> R: prompt_async
    K -> M: manager guidance
  end
else gate pass
  M -> K: request manager review (merge readiness)
  K -> R: prompt_async
  K -> M: approval_packet_ref

  M -> K: request USER approval (Merge)
  K -> User: approval UI (Merge)
  User -> K: Approve

  M -> K: merge worktrees

  M -> K: request manager review (release readiness)
  K -> R: prompt_async
  K -> M: release_packet_ref

  M -> K: request USER approval (Release)
  K -> User: approval UI (Release)
  User -> K: Approve

  M -> K: archive artifacts + update docs repo
end

@enduml
```

### 4.8 병렬 실행 정책(기본값)

- **Writer 1, Manager 1, Readers N**
  - 같은 repo/worktree에서 병렬 writer는 충돌 위험이 크므로 기본 금지
  - 읽기 워커는 병렬 허용
  - Manager는 checkpoint 리뷰 중심이므로 기본 1개(필요 시 분화)
- 병렬 writer가 꼭 필요하면
  - worktree를 추가 생성해 writer를 분리 배치
  - 최종 선택/병합은 gate로 검증

### 4.9 Manager 출력 포맷(정형)

Manager는 “결론”만이 아니라, 사용자가 승인할 수 있는 근거를 함께 제공해야 한다.

권장 포맷(개념)

- `approval_packet`
  - summary: 작업 요약
  - scope: 변경 범위(파일, 모듈, API)
  - risks: 리스크 및 완화책
  - checks: 수행한 검증(게이트/테스트)과 결과
  - missing: 누락된 검증과 이유
  - decision_points: 사용자 결정이 필요한 항목
  - recommendation: 승인/보류/수정요청 + 이유
  - artifact_refs: 근거 링크

예시(YAML 스니펫)

```yaml
approval_packet:
  entity: TASK-283
  checkpoint: merge
  summary: "로그인 실패 버그 수정 및 회귀 테스트 추가"
  scope:
    changed_files: 6
    api_change: false
  checks:
    lint: pass
    test: pass
    validator: pass
  risks:
    - "세션 만료 처리 변경으로 기존 클라이언트 호환성 영향 가능"
  missing:
    - "E2E 테스트는 프로젝트에 부재"
  decision_points:
    - "E2E가 없으므로 이번 릴리즈에 포함할지 여부"
  recommendation: "approve"
  artifact_refs:
    - "artifact:sha256:..."
```

### 4.10 예시 시나리오

#### 예시 A: 단순 lint 실패 수정(Manager 불개입)

1) Gatekeeper가 lint fail 보고
2) Moderator가 Fixer worker에 바로 delegate
3) Fixer가 수정 후 gate 재실행
4) lint pass면 다음 단계로 진행(단, merge 승인 전에는 Manager가 최소 리뷰)

#### 예시 B: API 변경을 수반하는 기능 추가(Manager 상시 개입)

1) Readers: API 영향 범위, 호환성 리스크 조사
2) Manager(Architect): 계약 변경 정책, 마이그레이션 계획 점검
3) TIS 승인 패킷 생성 후 사용자 승인
4) Writer 구현, Gatekeeper 검증
5) Manager(QA): 회귀/문서/API 버전 정책 점검 후 merge 승인 패킷

#### 예시 C: Brownfield import 후 갭 보강

1) Workers(Reader): PRD/기존 문서 파싱 + 코드 스캔
2) Manager(Brownfield): 문서-코드 갭을 “승인 가능한 Task”로 재구성
3) 사용자 승인 후, 우선순위 Task부터 aao 방식으로 실행

---

## 5. Gate와 Ralph loop

### 5.1 Gate 정의(커맨드 기반이 MVP 기본)

프로젝트별로 다음 Gate를 정의할 수 있어야 한다.

- lint
- validator (타입체크, 스키마 검증, 문서 검증 등)
- test
- LSP diagnostics(가능한 경우)

#### 5.1.1 MVP Gate 설정 방식(확정)

- **MVP은 “프로젝트 커맨드”만 지원**한다.
- 언어별 프리셋(JS/TS, Go 등)은 v1.x에서 추가한다.

Gate 설정 예시(개념)

| Gate | 예시 커맨드 | Pass 기준 |
|---|---|---|
| lint | `npm run lint` / `golangci-lint run` | exit 0 |
| validator | `npm run typecheck` / `openapi validate` / `buf lint` | exit 0 |
| test | `npm test` / `go test ./...` | exit 0 |
| lsp diagnostics | OpenCode LSP 수집 | count == 0 |

Gate 설정 파일 예시(초안)

```json
{
  "gates": {
    "lint": {"cmd": "npm run lint"},
    "validator": {"cmd": "npm run typecheck"},
    "test": {"cmd": "npm test"},
    "lspDiagnostics": {"max": 0}
  }
}
```

### 5.2 Ralph loop: 종료 조건/반복/병렬 파라미터

- 사용자가 Task 실행 요청 시 다음을 지정할 수 있어야 한다.
  - 종료 조건(until)
  - 최대 반복 횟수(maxIterations)
  - 병렬 worker 수(parallel.maxWorkers)
  - worker 타입 및 모델 믹스(pool)

#### 5.2.1 Loop Spec DSL 예시(텍스트)

```bash
/task "로그인 실패 버그 수정" \
  --until "tests=pass && lint=pass && validator=pass && lsp_diagnostics=0" \
  --max-iter 12 \
  --parallel 3 \
  --workers "gemini:1, glm:1, codex:1"
```

#### 5.2.2 Loop Spec JSON 예시(Web API)

```json
{
  "task": "로그인 실패 버그 수정",
  "loop": {
    "until": {
      "tests": "pass",
      "lint": "pass",
      "validator": "pass",
      "lspDiagnostics": 0
    },
    "maxIterations": 12,
    "parallel": {
      "maxWorkers": 3,
      "pool": [
        {"type": "gemini", "count": 1, "model": "google/gemini-*-*", "variant": "high"},
        {"type": "glm", "count": 1, "model": "zhipu/glm-*", "variant": "high"},
        {"type": "codex", "count": 1, "model": "openai/gpt-5.2-codex", "variant": "high"}
      ]
    }
  }
}
```

### 5.3 실패 정책(권장 기본)

- Gate 실패: iteration을 증가시키며 자동 재시도
- maxIterations 도달: `Failed`로 종료하되,
  - 실패 원인 요약(Top-N)
  - 마지막 gate 결과
  - 재시도 전략(예: 모델/워커 믹스 변경, scope 분할)
  - 변경 필요 시 CR(변경 요청) 제안
  을 **report artifact**로 남긴다.

---

## 6. 멀티 리포 및 Worktree

### 6.1 멀티 리포 Workspace 요구사항

- 하나의 Workspace는 여러 Repo를 등록할 수 있다.
  - server repo
  - client repo
  - docs repo
- Task는 repo scope를 가진다.
  - server only, client only, docs only, server+docs, server+client+docs 등

### 6.2 “동시에 둘 이상의 디렉토리” 처리 전략(옵션 비교)

| 옵션 | 요약 | 장점 | 리스크/단점 | MVP |
|---|---|---|---|---|
| A | 상위 폴더 1개에 여러 repo를 물리적으로 넣고, OpenCode를 상위에서 실행 | 단일 세션에서 탐색이 쉬움 | git/LSP 경계가 흐려질 수 있음 | 보조 수단 |
| B | 외부 디렉토리 접근을 권한으로 허용(예: docs는 read-only) | repo 구조 변경 최소화 | 세션/디렉토리 컨텍스트 이슈 시 위험 | 보조 수단 |
| C | **repo(worktree)별 OpenCode 백엔드 분리 + kkachi가 통합** | 경계 명확, 병렬 운영 유리 | 컨트롤 플레인 구현 필요 | **기본값(확정)** |

### 6.3 Worktree 기반 격리(기본)

- Track 또는 Task 실행 시, repo별로 worktree를 생성해 격리
- writer worker는 해당 worktree 범위에서만 쓰기 가능
- 완료 시 merge 승인 후 기본 브랜치로 병합

#### 6.3.1 예시: server + docs 동시 작업

- server repo: `wt/TRK-2026-0007/server`
- docs repo: `wt/TRK-2026-0007/docs`
- Gate: server(test/lint) + docs(validator: markdown lint, link check)

### 6.4 한 Workspace에서 둘 이상의 Task 동시 진행(동시성 모델)

- 가능하다. 단, 충돌 방지를 위해 기본 정책은 아래로 고정한다.
  - **Task마다 worktree를 분리**
  - **worktree 당 writer 1**
  - Gate는 Task별로 독립 수행
  - merge는 승인 후 순차(또는 repo별 병렬) 수행

예시

- TASK-132: server repo 변경 (server worktree A)
- TASK-198: client repo 변경 (client worktree B)
- 두 Task는 서로 다른 repo/worktree면 동시 실행 가능

### 6.5 “docs repo를 여러 Workspace가 공유”할 때의 동작 규칙

- kkachi 서버 관점에서 docs repo는 보통 다음 중 하나로 운영된다.

| 운영 방식 | 의미 | C workspace가 별도 fetch/rebase가 필요한가 |
|---|---|---|
| (1) 동일 clone + worktree 공유(서버 내부) | kkachi가 하나의 docs repo clone을 관리하고, 작업은 worktree로 분기 | 네트워크 fetch는 불필요. 다만 **다른 worktree가 main에 merge한 커밋을 내 worktree에 반영하려면 merge/rebase가 필요** |
| (2) Workspace마다 별도 clone | server/client/docs를 Workspace별로 각자 clone | 원격(origin)에서 fetch/pull 필요 |

MVP 기본값 권장

- (1) **서버 내부에서 동일 clone을 공유하고, Track/Task마다 worktree를 생성**
- 그리고 kkachi가 아래를 자동화
  - main 브랜치 fast-forward 갱신
  - 새 worktree는 항상 최신 main 기준에서 생성
  - 다른 worktree에서 main으로 merge가 발생하면, 영향받는 worktree에 “업데이트 필요” 이벤트를 남기고 사용자에게 선택지 제공

---

### 6.6 Worktree Lifecycle 관리(backup/restore/cleanup/quota) (MVP 포함)

worktree 기반 격리는 병렬성과 감사에는 유리하지만, 장기 운영에서는 디스크 누수와 “어떤 상태에서 실행됐는지” 재현 문제가 발생하기 쉽다. 따라서 MVP부터 최소한의 lifecycle 정책과 명령군을 표준화한다.

**필수 명령군(권장 API)**

- `worktree_new`: 최신 main 기준으로 새 worktree 생성
- `worktree_snapshot`: 실행 시점의 상태를 snapshot artifact로 기록(기준 커밋, diff 요약, 옵션)
- `worktree_restore`: snapshot 기준으로 worktree를 재구성(재현/디버깅 목적)
- `worktree_merge`: 승인 후 기본 브랜치로 병합
- `worktree_cleanup`: TTL/Quota 정책에 따라 worktree 정리(dry-run 지원)
- (선택) `worktree_gc`: 디스크 공간 회수(git gc 등)

**기본 정책(권장 기본값, Workspace 설정으로 조정 가능)**

- TTL
  - Done worktree: 7일 후 cleanup 대상
  - Failed worktree: 14일 후 cleanup 대상(재현 필요성이 더 높다고 가정)
  - Running worktree: cleanup 금지
- Quota
  - Workspace별 디스크 상한(예: 50GB) + 초과 시 가장 오래된 Done부터 정리
- Safety
  - Strict Approval 모드에서는 cleanup도 “예정 목록(dry-run)”을 먼저 보여주고 사용자 확인 후 수행 가능
  - All Allow 모드에서는 정책에 따라 자동 cleanup을 허용하되, 삭제 이벤트는 history에 기록

## 7. Tool Search(MCP 컨텍스트 절약)

### 7.1 문제 정의

- MCP 서버/도구가 많아질수록 도구 정의(설명/스키마)가 컨텍스트를 크게 점유
- 목표: “도구를 전부 넣지 말고, 필요할 때 검색으로 불러오는 지연 로딩(lazy loading)”

### 7.2 MVP 구현 방향(확정)

- kkachi가 **SQLite(FTS) 기반 Tool Registry**를 직접 구축한다.
- OpenCode 세션에는 최소 메타툴만 노출한다.
  - `tool_search(query, tags, provider, mcp_server)`
  - `tool_info(tool_id)`
  - `tool_call(tool_id, args)`

#### 7.2.1 Tool Registry 저장(예시)

- tools(id, name, description, schema_json, tags_json, provider, mcp_server, updated_at)
- tool_aliases(tool_id, alias)
- tool_usage_stats(tool_id, used_count, last_used_at)

#### 7.2.2 tool_search API 예시

- 입력: `"openapi validate"`, tags=`["validator"]`
- 출력: tool_id 목록 + 간단 요약
- tool_info: 선택한 tool_id의 전체 JSON schema 반환

#### 7.2.3 tool_search 랭킹 기준(MVP)

tool_search는 “관련성”만으로는 품질이 부족해지기 쉬우므로, MVP부터 아래의 **혼합 랭킹**을 권장한다.

- **기본 관련성(relevance)**: BM25 기반
  - BM25는 검색(query)과 문서(tool name/description/tags)가 공유하는 키워드의 “정보량”을 점수화하는 전통적인 텍스트 검색 랭킹 함수다.
  - SQLite FTS 계열에서도 유사한 BM25 스타일 랭킹을 사용할 수 있다.
- **최근 사용(recency)**: 최근 30일/7일 사용량 및 last_used_at
- **프로젝트 선호(workspace affinity)**: 해당 workspace에서의 사용량
- (선택) **성공률/에러율(quality)**: 실행 성공률이 낮은 도구는 감점

권장 스코어(예시)

```
score = w1*bm25 + w2*recency + w3*affinity + w4*quality
```

MVP 기본 정책(권장)

- w2, w3를 높게 두어 “최근 사용/프로젝트 선호”를 우선한다.
- 단, bm25가 0에 가까운(텍스트 상 무관한) 도구는 상위로 올라오지 않도록 하한선을 둔다.

### 7.3 OpenCode 플러그인/스킬과의 관계

- OpenCode 플러그인(opencode-toolbox 등)은 “패턴 참고” 또는 “초기 부트스트랩” 용도로만 활용 가능
- MVP의 정본은 kkachi Tool Registry이며, kkachi가 검색 결과/사용 로그를 history에 남긴다.

---

## 8. 플러그인 전략(oh-my-opencode 제외)

### 8.1 MVP 기본 번들(확정)

| 목적 | 플러그인 | 비고 |
|---|---|---|
| 동적 컨텍스트 프루닝 | opencode-dynamic-context-pruning(DCP) | 툴 출력/로그 정리로 토큰 절감 |
| 웹서치 + 인용 | opencode-websearch-cited | 조사 근거를 아티팩트로 보존 |
| 장기 실행 안정화 | opencode-pty | 서버 환경 long-running 안정화 |
| 셸 hang 방지 | opencode-shell-strategy | Gate/루프 신뢰성 개선 |
| 대형 편집 가속 | opencode-morph-fast-apply | 반복 수정 성능 개선 |
| 알림 | opencode-notifier 또는 opencode-notificator | Web UI 알림과 병행 가능 |
| 세션 간 개인 메모리 | opencode-supermemory | kkachi 히스토리와 별도(개인) |
| 스킬 lazy load | opencode-skillful | Operator를 스킬로 배포 시 유리 |
| md 테이블 정리 | opencode-md-table-formatter | 보고서 품질 개선 |

### 8.2 인증 플러그인(주의)

- opencode-antigravity-auth 류는 제공자 정책/ToS 리스크가 있을 수 있으므로 MVP 기본 번들에는 넣지 않고, **사용자 책임 옵션**으로 둔다.

---

## 9. OpenCode 통합 디테일(관측/제어)

### 9.1 OpenCode 서버/API 활용 포인트(대화에서 언급)

- 세션 생성/조회/상태
  - `POST /session`
  - `GET /session/status`
- 비동기 실행
  - `POST /session/:id/prompt_async`
- 이벤트 스트림(SSE)
  - `GET /global/event` (또는 유사 엔드포인트)
- 결과 수집
  - `GET /session/:id/diff`
- 메시지 주입(메인 세션에 worker 결과 전달)
  - `POST /session/:id/message`
- 중단
  - `POST /session/:id/abort`

> 실제 엔드포인트/스키마는 OpenCode 버전에 따라 다를 수 있으므로, MVP 착수 시 “OpenCode API 계약 확인”을 1차 작업으로 둔다.

### 9.2 OpenCode 플러그인 훅(이벤트 예)

- `tool.execute.before/after`
- `session.*`
- `lsp.*`
- `tui.*`

kkachi는 훅을 다음 목적에 사용한다.

- 관측/로그 수집(도구 호출, 에러, LSP 진단)
- 컨텍스트/규칙 주입(권한/스코프 고정)
- 루프 안전장치

---

## 10. 핵심 워크플로우

### 10.1 신규 프로젝트(그린필드)

1) 사용자가 목표 입력
2) Interview Mode로 요구사항 수집 및 구조화
3) Spec/Track/Task(의존성 포함 Task Graph) 생성
4) Manager(Architect) 리뷰 및 Approval Packet 생성
5) **Spec 승인(Approval Gate)**
6) 승인된 순서대로 진행 또는 사용자가 특정 Task 선택
7) 완료 시 docs 업데이트 및 history 아카이브

### 10.2 기존 프로젝트(브라운필드) 전환(import → normalize → interview)

1) PRD/Task 목록/진행 문서 입력
2) 문서 파싱 + 코드 분석(LSP, 검색, 테스트)으로 kkachi 포맷 변환
3) 문서-코드 갭(누락 요구사항, 불일치, 테스트 부재) 목록화
4) 부족분만 추가 인터뷰로 보강
5) Manager(Brownfield) 리뷰 및 Approval Packet 생성
6) **Spec 승인** 후 실행

### 10.3 Task 실행(예: TASK-283)

#### 10.3.1 표준 프로세스(SOP, MVP 기본)

1) Task 설계 요청 접수
2) 애매하거나 사용자 결정이 필요한 부분 인터뷰
3) Task Implementation Spec(TIS) 작성
4) Manager(Task Spec) 리뷰 및 Approval Packet 생성
5) **Task 승인(Approval Gate)**
6) 구현(Writer) + 병렬 조사(Readers)
7) Gate 실행
8) 실패 시 Ralph loop 반복
9) 성공 시 Done 처리
10) Manager(QA) 리뷰 및 Approval Packet 생성
11) **Merge 승인(Approval Gate)**
12) merge 수행(worktree → default branch)
13) Manager(Verification) 리뷰 및 Release Packet 생성
14) **Release 승인(Approval Gate)** (릴리즈가 정의된 프로젝트인 경우)
15) docs 업데이트 및 immutable history 스냅샷

#### 10.3.2 All Allow 모드 변형(설계 -> 구현 -> 검증 원샷)

All Allow 모드에서는 아래 항목이 Strict Approval와 다르게 동작한다.

- **승인 게이트 자동 승인**
  - Spec/Task/Merge/Release에서 사용자의 클릭을 기다리지 않고 진행한다.
  - 대신 각 승인 지점마다 Manager가 Approval Packet을 생성하고, kkachi는 이를 history에 저장한 뒤 “system auto-approve” 이벤트를 append한다.

- **Interview의 비차단 처리(assumption-driven)**
  - 인터뷰 질문을 “대기”하지 않고, 선택지/장단점/권장안을 제시한 뒤 권장안으로 진행한다.
  - 답변이 필요한 결정은 decision_points로 기록한다.
  - 사용자가 이후에 다른 선택을 원하면 CR(Change Request)로 되돌리거나, 다음 iteration에서 조정한다.

- **안전장치(Auto-Pause)**
  - All Allow 모드에서는 0.10의 **Auto-Pause 승격 정책**을 적용한다.
  - 감지 신호가 임계값을 넘으면 자동으로 `Paused` 상태로 전환하고, **Pause Packet**을 생성하여 Approval Center에 노출한다.
  - 대표 트리거: 보안/권한/시크릿/데이터 마이그레이션/대규모 삭제/반복 실패/충돌/파괴적 커맨드.

#### 10.3.3 테스트/품질 기준(가이드)

- 최소: happy path + 중요한 edge case
- 회귀: 기존 테스트 실패가 없어야 함
- 리팩터/중복 제거: loop 중 장기적으로 반복되는 원인(코드 중복, 구조 결함)이면 Refactorer 투입
- 문서 업데이트: 변경이 사용자에게 노출되는 기능이면 docs repo 업데이트를 동반

---

## 11. Operator(초기 9개) 정의

MVP은 아래 9개의 Operator를 기본 역할(Role)로 정의한다.

> **Operator(Role)** 와 **Actor Class(Moderator/Manager/Worker)** 를 분리해서 본다.
>
> - Orchestrator는 Moderator가 수행
> - Reviewer/Architect 계열은 Manager가 수행
> - 나머지 실행은 Worker가 수행

1) **Orchestrator**: 작업 분해, 워커 배치, 루프/게이트 총괄
2) **SpecWriter**: spec/plan 및 TIS 작성(문서 반영은 docs-scope writer worker)
3) **Librarian(Read)**: docs repo/kkachi history 조회 및 검색, 근거 수집, 관련 링크/아티팩트 정리(읽기 전용)
4) **CompatChecker**: server-client 계약, API/타입 호환성 점검
5) **Explorer**: LSP/검색 기반 코드 탐색, 원인 규명
6) **Fixer**: 작은 변경 위주의 구현/버그 수정(writer)
7) **Refactorer**: 구조 개선 및 큰 변경(writer)
8) **Gatekeeper**: lint/validator/test 실행 및 실패 분석
9) **Reviewer**: 변경 검토, 리스크/추가 TODO, 승인 패킷 작성(주로 Manager)

추가 원칙

- LibrarianWriter(Write)는 별도 operator로 고정하기보다, **docs-scope Writer 권한 프로파일**로 제공한다(필요 시 SpecWriter/DcosWriter 역할로 위임).
참고

- docs 업데이트는 “별도 Operator”로 늘리기보다, **Writer의 권한 프로파일을 docs-scope로 제한**하는 방식을 MVP 권장
- **Architect(Role)** 는 Manager가 담당하는 대표 Role로 유지한다.
  - Greenfield/Brownfield 설계 감독
  - 스코프/리스크/일관성 점검

### 11.1 (선택) 추가 Operator 후보(확장 아이디어)

- **DocsWriter**: docs repo만 쓰기 허용, 문서 품질 게이트 강화
- **ReleaseManager**: 배포/릴리즈 노트/태그/버전 정책 자동화
- **E2ETester**: 통합/E2E 테스트 시나리오 생성 및 실행
- **SecurityAuditor**: 의존성 취약점, 권한, 시크릿 스캔

---

## 12. 모델 라우팅(Agent-Model 가변 연결)

### 12.1 요구사항

- Operator별 모델을 런타임에 변경 가능해야 한다.
- 사용자는 Task 실행 시 아래를 지정할 수 있다.
  - Orchestrator 모델
  - Manager 모델
  - worker pool 구성(타입별 개수, 최대 병렬 수)

### 12.2 “세션 시작 후 모델 변경” 정책(명시)

- MVP에서 권장 정책
  - **prompt 호출 단위로 모델 지정 가능**(즉, 같은 세션이라도 다음 prompt부터 모델을 바꿀 수 있음)
  - 단, 재현성과 감사(audit)를 위해 **모든 worker_job에 model_ref를 기록**
- 운영 정책 옵션
  - (A) iteration 경계에서만 모델 변경 허용
  - (B) 언제든 변경 허용(단, 변경 이벤트를 history에 남김)

### 12.3 provider 전환과 컨텍스트 유지(질문 반영)

세션 내에서 “모델만 변경”하는 것과 “provider까지 변경”하는 것은 다르게 취급한다.

- **동일 provider 내 모델 변경(예: 같은 OpenAI 계열 모델 간 전환)**
  - 같은 OpenCode 세션을 유지하는 한, 대화 컨텍스트는 일반적으로 유지된다.
  - 단, 모델마다 응답 품질/정책이 달라질 수 있으므로 모든 worker_job에 model_ref를 기록한다.

- **서로 다른 provider로 전환(예: glm -> gemini)**
  - OpenCode 내부적으로 동일한 컨텍스트를 “그대로” 유지한다고 가정하지 않는다.
  - kkachi는 새 세션을 생성하고, **Canonical State Packet(CSP)** 으로 컨텍스트를 재구성한다.
  - 전환 이벤트(session_handoff)는 history에 기록한다.

### 12.4 권장 기본 라우팅 프리셋(v0.5)

| 구분 | 기본 모델 티어 | 승격 조건(예시) | 비고 |
|---|---|---|---|
| Moderator(Orchestrator) | medium | 작업이 대형 설계로 전환될 때 | Moderator는 상태/위임이 주 역할 |
| Manager(Architect/Reviewer) | high | 항상 high 권장 | 승인 패킷과 리스크 판단이 품질을 좌우 |
| Worker(Reader) | low~medium | 분석 난이도 상승, 정보량 과다 | 대규모 코드베이스 탐색 시 medium 권장 |
| Worker(Writer) | medium | 리팩터/대규모 변경, 반복 실패 | 기본은 medium, 필요한 경우에만 high |
| Worker(Gatekeeper) | low~medium | 로그가 길고 분석이 필요할 때 | 실행은 low, 해석은 medium로 분리 가능 |

### 12.5 토큰 절감 장치(정책)

- **Manager는 항상 상주하지 않는다**
  - 체크포인트(승인 전)와 에스컬레이션 상황에서만 호출
- **DCP(opencode-dynamic-context-pruning)**
  - 툴 출력/게이트 로그/장문을 요약하여 컨텍스트 점유를 줄임
- **Tool Search(Registry)**
  - 도구 정의를 상시 컨텍스트에 넣지 않고, 필요 시 검색으로 지연 로딩
- **Gate 결과의 구조화 저장**
  - 반복 루프에서 “지난 결과 재설명”을 최소화

---

## 13. Spec/Track/Task 문서 관리(문서 repo)

### 13.1 문서 원칙

- Spec/Plan은 사람이 리뷰 가능한 Markdown 중심
- 작업 중 문서는 변경 가능하되 버전화/승인 이력 기록
- 완료 후 산출물은 history 영역으로 이동하거나 불변 스냅샷으로 고정
- docs repo는 Workspace 문서의 **정본(canonical)** 이다. kkachi는 상태/이벤트를 SQLite에 저장하되, Spec/Track/Task 및 설계 문서는 docs repo에 유지한다.
- kkachi 서버(제품 코드)는 프로젝트별 관리 파일을 자체 저장소에 별도로 두지 않고, clone/worktree 영역에서만 작업한다.

### 13.2 kkachi 고유 문서 repo 구조(확정)

> MVP에서 표준 디렉터리는 `kkachi/` 를 사용한다(숨김 디렉터리 대신 명시적).

권장 레이아웃(예)

```
docs-repo/
  kkachi/
    workspace.json
    tracks/
      TRK-2026-0007/
        spec/
          spec.v1.md
          spec.v2.md
        plan/
          plan.v1.md
        tasks/
          TASK-283/
            tis.v1.md
            notes.md
        approvals/
          approvals.jsonl
        history/
          run_0001/
            worker_read_0001.md
            worker_write_0002.md
            gate_test.log
            gate_lint.log
            summary.json
        final/
          report.md
          gates.json
  architecture/
    overview.md
    diagrams/
  adr/
    ADR-0001-*.md
  scenarios/
    user-scenarios.md
```

### 13.3 Tag/추적성(선택 기능)

- 코드 변경과 Spec/Task를 연결하기 위한 Tag 지원
  - 예: 커밋 메시지 또는 코드 주석에 `@TRACK:TRK-2026-0007` / `@TASK:TASK-283`
- Gate 단계에서 태그 존재/정합성을 검증하는 옵션 제공

---

## 14. History(불변 이력)와 분석

### 14.1 목표

- worker 실행, gate 결과, 의사결정, diff 요약 등을 Task 단위로 누적 보관
- 완료된 작업은 수정 불가로 고정, 나중에 검색/분석 가능

### 14.2 저장 단위(권장)

- Human-readable 보고서(필수)
- Machine-readable 메타(필수)
- Raw transcript(선택)

---

## 15. 변경 관리(Change Management, 피봇 지원)

### 15.1 요구사항

- Archived history는 수정 불가
- Draft/Proposed/Approved/Ready/Running 상태의 Spec/Track/Task는 CR을 통해 수정/재구성 가능
- 변경은 단순 편집이 아니라 버전/대체(supersede)로 모델링

### 15.2 Change Request(CR)

- 변경 유형(minor/major/pivot/reorder/scope-cut)
- 변경 사유(Why)
- 영향 범위(대상 Spec/Track/Task, repo)
- 영향 분석(취소/대체/추가되는 Task, 의존성 변화)
- 전환 계획(마이그레이션, 롤백)
- 변경안 승인

### 15.3 Running Task 처리(사용자 결정 반영)

- CR이 들어왔고 Running task가 있는 경우, kkachi는 기본적으로 **Pause + Snapshot**을 수행
- 이후 사용자가 선택
  - 계속 진행(Resume)
  - Abort + Snapshot
  - Salvage(재사용 가능한 변경을 신규 Task로 이관)

### 15.4 UI 지원(필수)

- Change Request Wizard
- Impact Report(변경 전/후 비교)
- Task Graph Diff
- Approval 기록 및 타임라인

---

## 16. Web UI 요구사항

### 16.1 화면 구성(최소)

- Workspace Dashboard
- Spec/PRD View
- Track Board
- Task Detail
- Run Timeline
- History/Archive
- **Dependency Graph View(추가)**: Track 내 Task 의존성 시각화
- **Approval Center(추가)**: Spec/Task/Merge/Release 승인 요청 큐(자동 승인/Auto-Pause 포함)
- **Decision Points(추가)**: All Allow에서 생성된 pending 결정 목록 확인/확정/CR 생성
- **Auto-Pause Incidents(추가)**: Auto-Pause 발생 내역, 원인 신호, 재개/전환 버튼
- **Approval Policy Mode 표시/전환**: Strict Approval vs All Allow (Workspace 기본값 + 실행 시 override)

### 16.2 UX 원칙

- 사용자는 “Moderator 1명이 끝까지 진행”하는 느낌 유지
- 병렬 worker는 내부적으로 실행, UI에는 상태와 핵심 결과만 요약
- 승인 없는 대규모 변경은 실행되지 않도록 가드

### 16.3 PlantUML: Task 의존성 그래프 예시

```plantuml
@startuml
title Track task dependencies (example)
left to right direction

rectangle "TASK-132\n(server)" as T132
rectangle "TASK-198\n(client)" as T198
rectangle "TASK-201\n(docs)" as T201

T132 --> T198 : API contract
T132 --> T201 : update API docs
T198 --> T201 : update UI docs
@enduml
```

---

## 17. 권한/안전장치 및 협업(멀티 유저)

> MVP는 **1인 사용**을 기본 타겟으로 한다. 다만 승인/감사/권한 모델은 향후 소규모 협업(<=10인)으로 확장 가능하도록 설계한다.

### 17.1 기본 원칙

- Moderator, Reader worker, Manager는 기본 read-only
- Writer worker만 파일 편집/커밋 권한
- docs repo 수정 권한은 docs-scope Task에서만
- 외부 디렉토리 접근은 기본 차단 또는 ask

### 17.2 인증/계정(1인 기본, 소규모 협업 확장)

- **MVP 권장**: Firebase Auth
  - 1인 사용에서도 동일한 인증 스택을 사용하면 배포/운영이 단순해진다.
  - 오프라인/로컬 개발을 위해 개발 모드(local bypass)는 선택으로 둘 수 있다.

- 단일 kkachi 서버를 여러 사용자가 함께 사용 가능
- 최소 필요 기능(MVP~v0.2)
  - 사용자 계정(로컬 또는 OAuth)
  - Workspace 멤버십과 역할(owner/maintainer/writer/reader)
  - 실행/승인 이벤트의 감사 로그(approvals/events)
  - 동시 실행 시 리소스 제한(세션 수, parallel maxWorkers)

---

## 18. MVP 구현 범위(MVP) 재정렬

### 18.1 포함

- Web UI: Workspace/Track/Task 목록, Task 실행, 상태/로그/게이트 표시
- Interview Mode: 신규 프로젝트 Spec/Track/Task 생성 + Spec 승인
- Import Mode: 기존 PRD/Task 입력 -> 변환 -> 갭 탐지 -> Spec 승인
- Moderator + Manager + worker session 런타임(allatonce aao)
- Gate + Ralph loop
- worktree 기반 실행 격리
- SQLite 기반 history(append-only) + docs repo 아티팩트 저장
- Change Request 기반 피봇 지원
- Tool Search(kkachi Tool Registry + 메타툴)
- **Approval Center**: Spec/Task/Merge/Release 승인 플로우
- **Approval Policy Mode**: Strict Approval vs All Allow (Workspace 기본값 + 실행 시 override)
- **MVP 기본 플러그인 번들 포함**(섹션 8.1)

### 18.2 제외 또는 옵션

- 고급 병렬 writer(자동 병합/충돌 해결) 완전화
- 고급 자동 릴리즈/배포 파이프라인
- Jira/Linear 등 외부 시스템 양방향 동기화

---

## 19. “대화에서 나온 플러그인/선택지” 정리

### 19.1 MVP 기본 번들(재표기)

- opencode-dynamic-context-pruning
- opencode-websearch-cited
- opencode-pty
- opencode-shell-strategy
- opencode-morph-fast-apply
- opencode-notifier 또는 opencode-notificator
- opencode-supermemory
- opencode-skillful
- opencode-md-table-formatter

### 19.2 kkachi가 직접 구현하는 핵심(경쟁력)

- Web UI + API(제품)
- Workspace(멀티 repo) 개념과 운영
- SQLite 불변 히스토리 + 분석
- Ralph loop + Gate 파이프라인
- allatonce(aao) 경험(Moderator 중심 fan-out/join/retrigger)
- Manager(승인/품질)
- Tool Search의 UX/정책/분석(플러그인 채택 여부와 무관)

---

## 20. 결정 로그(질문과 답변)

1) 

## 21. 추가 및 확인 필요 사항 (사용자 질문 + 답변/반영 위치)

1) 

## Appendix A. Schemas (JSON Schema 초안)

> 아래 스키마는 구현 착수 시점에 고정하기 위한 초안이며, MVP에서는 “필수 필드” 중심으로 우선 적용한다.

### A.1 Approval Packet (MVP minimal)

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "ApprovalPacket",
  "type": "object",
  "required": ["entity", "checkpoint", "summary", "scope", "checks", "risks", "recommendation", "artifact_refs"],
  "properties": {
    "entity": {"type": "string"},
    "checkpoint": {"type": "string"},
    "summary": {"type": "string"},
    "scope": {
      "type": "object",
      "properties": {
        "changed_files": {"type": "integer", "minimum": 0},
        "files": {"type": "array", "items": {"type": "string"}},
        "api_change": {"type": "boolean"}
      }
    },
    "checks": {
      "type": "object",
      "properties": {
        "lint": {"type": "string", "enum": ["pass", "fail", "skip"]},
        "validator": {"type": "string", "enum": ["pass", "fail", "skip"]},
        "test": {"type": "string", "enum": ["pass", "fail", "skip"]},
        "lsp_diagnostics": {"type": "integer", "minimum": 0},
        "commands": {"type": "array", "items": {"type": "string"}}
      }
    },
    "risks": {"type": "array", "items": {"type": "string"}},
    "missing": {"type": "array", "items": {"type": "string"}},
    "decision_points": {"type": "array", "items": {"type": "string"}},
    "recommendation": {"type": "string", "enum": ["approve", "hold", "revise"]},
    "artifact_refs": {"type": "array", "items": {"type": "string"}}
  }
}
```

### A.2 Pause Packet (MVP minimal)

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "PausePacket",
  "type": "object",
  "required": ["entity", "phase", "triggered_by", "summary", "recommended_action", "evidence_refs"],
  "properties": {
    "entity": {"type": "string"},
    "phase": {"type": "string"},
    "triggered_by": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["signal"],
        "properties": {
          "signal": {"type": "string"},
          "value": {},
          "threshold": {},
          "severity": {"type": "string", "enum": ["info", "warn", "critical"]}
        }
      }
    },
    "summary": {"type": "string"},
    "risks": {"type": "array", "items": {"type": "string"}},
    "recommended_action": {"type": "string", "enum": ["continue", "switch_to_strict", "abort", "create_cr"]},
    "evidence_refs": {"type": "array", "items": {"type": "string"}}
  }
}
```

### A.3 Decision Point (MVP minimal)

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "DecisionPoint",
  "type": "object",
  "required": ["id", "topic", "question", "options", "recommended", "chosen", "chosen_by", "status"],
  "properties": {
    "id": {"type": "string"},
    "topic": {"type": "string"},
    "question": {"type": "string"},
    "options": {"type": "array", "items": {"type": "object"}},
    "recommended": {"type": "string"},
    "chosen": {"type": "string"},
    "chosen_by": {"type": "string", "enum": ["user", "system"]},
    "status": {"type": "string", "enum": ["pending_user_confirm", "confirmed", "overridden"]},
    "evidence_refs": {"type": "array", "items": {"type": "string"}}
  }
}
```

## References

- <https://github.com/modu-ai/moai-adk?tab=readme-ov-file>
- <https://github.com/Yeachan-Heo/oh-my-claude-sisyphus>
- <https://github.com/code-yeongyu/oh-my-opencode>
- <https://github.com/gemini-cli-extensions/conductor>
