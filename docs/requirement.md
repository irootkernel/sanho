# Kkachi Web v2 Requirement

## 1. 개요

### 1.1 목적

Kkachi Web v2는 기존 kkachi-server / kkachi CLI 위에 **상태 가시화를 위한 Web UI 대시보드**를 제공하는 것을 목표로 한다.  

- v1에서 정의된 중앙 문서 조정 시스템(kkachi-server)과 workspace 기반 CLI 워크플로우는 그대로 유지한다.:contentReference[oaicite:0]{index=0}  
- v2는 **읽기 전용(Read-only)** Web UI를 통해 다음을 제공한다.
  - project별 docs HEAD 및 workspace 상태 시각화
  - 각 workspace의 docs 기준 버전과 서버 HEAD의 불일치 여부 표시
  - 마지막 동기화 시각, 마지막 actor 등의 메타데이터 제공:contentReference[oaicite:1]{index=1}  

v2에서는 **어떠한 server-side 쓰기 작업도 Web에서 수행하지 않으며**, 문서 업데이트는 여전히 kkachi CLI + Git hook을 통해서만 이루어진다.:contentReference[oaicite:2]{index=2}  

### 1.2 범위

이 문서는 다음 범위를 포함한다.

- Web UI (React + TypeScript + Vite) 요구사항
- Web UI를 위한 서버 측 최소 확장(정적 파일 서빙, `/api` alias 등)
- UX / 상태 표시 규약
- 비기능 요구사항(테스트, 배포, 보안)

다음은 범위에서 제외한다.

- Task / Agent / Run / MCP 등 v4 이후 기능
- Web에서 docs 내용을 직접 편집하는 기능
- Web에서 프로젝트 / workspace / docs repo를 생성·삭제하는 기능

### 1.3 전제 및 의존 관계

- v1 기준 kkachi-server 기능 및 상태 모델(`docs_repos`, `project_to_docs_repo`, `workspaces`)이 이미 구현되어 있다.  
- v1 기준 kkachi CLI 및 Git hook에 의한 워크플로우가 운영 중이다.:contentReference[oaicite:4]{index=4}  
- v2는 **서버의 기존 REST API**(특히 `/state`, `/docs/head`) 위에 구축된다. 새로운 쓰기 API는 정의하지 않는다.  
- 배포 환경은 개인 또는 소규모 팀의 **로컬 네트워크**를 전제로 하며, 인증/인가 없이 모든 사용자가 Web에 접근가능하다고 가정한다.

---

## 2. 용어 정의

### 2.1 기존 용어 상속

다음 용어는 v1 requirement에서 정의한 의미를 그대로 사용한다.:contentReference[oaicite:6]{index=6}  

| 용어 | 의미 |
|---|---|
| project | kkachi가 관리하는 논리적 프로젝트 이름 (예: `sudal`, `dolgorae`) |
| docs repo | 각 project의 문서 원본을 저장하는 전용 Git repo (예: `sudal_docs`) |
| workspace | kkachi에 등록된 하나의 작업 디렉토리 (예: `/Users/karl/dev/sudal`) |
| kkachi-server | docs repo를 실제로 clone/관리하고 REST API를 제공하는 중앙 서버 |
| kkachi CLI | 각 workspace에서 실행되는 CLI 바이너리로, init / status / fix / hook 등을 제공 |
| docs_hash | workspace가 기준으로 삼는 docs repo commit hash (`.kkachi_docs_hash`) |

### 2.2 Web v2 신규 용어

| 용어 | 의미 |
|---|---|
| Web UI / kkachi-web | React + TypeScript + Vite로 구현되는 Kkachi Web 프론트엔드 |
| Dashboard | project / workspace 상태를 한 눈에 보여주는 메인 화면 |
| Status Badge | workspace 상태를 시각적으로 표현하는 작은 UI 요소 (색상/텍스트) |
| Outdated | workspace docs 기준 버전(`docs_hash`)과 서버 docs HEAD가 다른 상태 |
| Up-to-date | workspace docs 기준 버전과 서버 docs HEAD가 동일한 상태 |

workspace의 상태 정의(Up-to-date / Outdated / Unknown)는 v1 CLI의 DocsStatus 정의와 개념적으로 일치해야 한다.:contentReference[oaicite:7]{index=7}  

---

## 3. 아키텍처 및 디렉토리 구조

### 3.1 컴포넌트 구성

v2에서의 주요 컴포넌트는 다음과 같다.

- **kkachi-server (기존)**  
  - `/state`, `/docs/head`, `/docs/snapshot`, `/docs/push` 등 REST API 제공  
- **kkachi-web (신규)**  
  - React + TypeScript SPA
  - REST API를 통해 kkachi-server와 통신
  - Dashboard / 프로젝트별 상세 화면 렌더링

데이터 흐름(고수준):

1. 브라우저에서 Web UI 로드
2. Web UI가 kkachi-server의 `/api/state`를 호출하여 전체 상태를 가져옴 (레거시/개발 환경에서는 `/state` fallback 가능)
3. Project / workspace 별로 상태를 가공해 화면에 출력

### 3.2 Monorepo 구조

v2에서는 server + web을 하나의 monorepo에서 관리한다.  

예시 디렉토리 구조:

```text
kkachi/
  cmd/
    server/           # kkachi-server 진입점
  internal/
    ...               # v1 server 코드
  web/                # v2 Web UI (React + TS + Vite)
    index.html
    src/
      main.tsx
      App.tsx
      components/
      pages/
      api/
    package.json
    vite.config.ts
````

* `web/` 디렉토리는 독립적인 Node 패키지로서 개발/빌드된다.
* `go build` 시 Web 빌드는 포함되지 않으며, 배포 단계에서 `npm run build` → 정적 파일을 서버가 서빙하는 방식으로 연동한다.

### 3.3 서버–클라이언트 통신 구조

* 기본 통신 방식: HTTP + JSON
* 기본 API 엔드포인트:

  * `GET /state` – 서버 상태 조회(v1 유지)
  * `GET /api/state` – v2 Web UI 기본 엔드포인트(`/state` alias, 동일 응답)
  * `GET /healthz` – v2 헬스 체크 엔드포인트(운영 필수)
* 동일 Origin을 사용한다 (예: `http://localhost:5789` 에서 서버와 Web UI 모두 제공).

---

## 4. 공통 UI/UX 원칙

### 4.1 UX 기본 원칙

* Web UI는 **읽기 전용** 상태 모니터링 도구이다.

  * v2에서는 어떠한 상태 변경/쓰기 작업도 제공하지 않는다.
* 사용자에게 중요한 것은:

  * 어떤 project가 존재하는지
  * 각 project/docs repo의 현재 HEAD가 무엇인지
  * 각 workspace가 최신 HEAD를 따라잡았는지(Up-to-date/Outdated)
  * 마지막 동기화가 언제였는지
* 복잡한 그래프/차트 보다는 **테이블 + 간단한 색상/뱃지** 위주로 구성한다.

### 4.2 상태 표현 규약

Web UI에서 workspace 상태는 다음과 같이 표현한다.

| 상태         | 판단 기준                                        | 표시 텍스트 예시    | 색상 예시   |
| ---------- | -------------------------------------------- | ------------ | ------- |
| Up-to-date | `workspace.docs_hash == docs_heads[project]` | `Up-to-date` | 녹색      |
| Outdated   | `workspace.docs_hash != docs_heads[project]` | `Outdated`   | 주황 / 빨강 |
| Unknown    | 서버 응답 없음 또는 project 매핑 없음                    | `Unknown`    | 회색      |

`docs_heads`와 `workspaces[].docs_hash`는 `/state` 응답 구조를 그대로 사용한다.

### 4.3 리프레시 정책

* 초기 버전(v2)에서는 **수동 리프레시 버튼**을 제공한다.

  * “새로고침” 버튼 클릭 시 `/api/state`를 다시 호출한다. (레거시/개발 환경에서는 `/state` fallback 가능)
* 자동 주기 리프레시는 선택 옵션이며, 초기 구현에서는 필수 아님.

  * 추후 `10초/30초` 자동 리프레시 옵션을 설정으로 추가할 수 있다.

---

## 5. 기능 요구사항

### 5.1 화면/기능 목록

| ID   | 이름            | 설명                                        | 우선순위 |
| ---- | ------------- | ----------------------------------------- | ---- |
| F2-1 | 프로젝트 리스트 화면   | 모든 project와 docs HEAD를 요약해서 보여주는 메인 대시보드  | 필수   |
| F2-2 | 프로젝트 상세 화면    | 특정 project에 속한 workspace 상태를 테이블로 보여주는 화면 | 필수   |
| F2-3 | 전체 상태(raw) 보기 | `/state` 응답을 개발자 친화적으로 보여주는 디버그용 화면       | 선택   |
| F2-4 | 에러/빈 상태 화면    | 서버 응답 실패, project 없음 등의 예외 상황 처리          | 필수   |

### 5.2 F2-1 프로젝트 리스트 화면

#### 5.2.1 목적

* 서버가 관리하는 **모든 project**의 상태를 한눈에 보여준다.
* 각 project의 docs HEAD와 workspace 수, Outdated workspace 수 등을 요약한다.

#### 5.2.2 동작 요구사항

* `GET /api/state` 호출 결과의 `docs_heads` 및 `workspaces`를 기반으로 각 project별로 다음을 계산한다. (레거시/개발 환경에서는 `/state` fallback 가능)

  * `docs_head` – `docs_heads[project]` (없으면 null)
  * `workspace_count` – 해당 project를 가진 workspace 개수
  * `unknown_count` – `docs_head`가 없는 project의 workspace 개수
  * `outdated_count` – `docs_head`가 있는 경우에만 `docs_hash != docs_head` 인 workspace 개수 (`docs_head` 없으면 0)
  * `last_reported_at_max` – 해당 project workspaces 중 가장 최근 `last_reported_at` 값

* 화면 요소:

  * 프로젝트 카드 리스트 또는 테이블

    * 컬럼 예시:

      * Project
      * Docs HEAD
      * Workspaces (총 개수)
      * Outdated (개수)
      * Last updated (가장 최근 `last_reported_at`)
    * 각 row/카드 클릭 시 F2-2 프로젝트 상세 화면으로 이동.

* 정렬/필터:

  * 기본 정렬: project 이름 오름차순
  * 선택 기능:

    * Outdated workspace가 있는 project(=docs_head 존재 & outdated_count > 0)를 상단에 배치 (정렬 옵션)

#### 5.2.3 빈 상태/에러 처리

* `/api/state`(=`/state`) 응답의 `workspaces`가 비어 있지만 `docs_heads`가 존재할 때:

  * 프로젝트 목록은 표시하되, 상단 배너로 “아직 workspace가 없습니다. 먼저 kkachi init 또는 kkachi workspace register를 실행해 주세요.” 정도의 안내 문구를 표시한다.
* `docs_heads`와 `workspaces`가 모두 비어 있는 “완전 빈 state”는 §5.5에서 전역 처리한다.
* `/api/state` 호출 실패 시(또는 `/state` fallback 실패 시):

  * 화면 상단에 에러 배너 표시 (예: “서버에 연결할 수 없습니다. retry 버튼을 누르거나 서버 상태를 확인해 주세요.”)
  * Retry 버튼 제공.

### 5.3 F2-2 프로젝트 상세 화면

#### 5.3.1 목적

* 특정 project에 속한 모든 workspace 상태를 세부적으로 보여준다.
* 각 workspace가 Up-to-date / Outdated 인지, 마지막 동기화 시점이 언제인지 파악하게 한다.

#### 5.3.2 동작 요구사항

* URL 구조 예시:

  * `/app/projects/:projectName`

* 진입 시:

  1. `/api/state` 를 호출한다 (이미 메인 화면에서 가져온 데이터가 있다면 캐시 재사용 가능; 레거시/개발 환경에서는 `/state` fallback 가능).
  2. `workspaces` 중 `project == :projectName` 인 항목만 필터링한다.
  3. `docs_heads[:projectName]` 값을 가져온다; 없으면 docs HEAD가 없는 project로 처리하며, workspace 상태는 Unknown으로 표시한다.

* 워크스페이스 테이블:

  | 컬럼            | 내용                            |
  | ------------- | ----------------------------- |
  | Workspace ID  | `workspace_id`                |
  | Docs Repo ID  | `docs_repo_id`                |
  | Local Path    | `local_path`                  |
  | Repo URL      | `repo_url`                    |
  | Docs Hash     | `docs_hash`                   |
  | Docs HEAD     | `docs_heads[project]` (없으면 `—`) |
  | Status        | Up-to-date / Outdated / Unknown (Badge) |
  | Last Reported | `last_reported_at`            |
  | Last Actor    | `last_actor_email`            |

* Status 계산 규칙:

  * `docs_heads[project]`가 없으면 → Unknown
  * `docs_hash == docs_heads[project]` → Up-to-date
  * `docs_hash != docs_heads[project]` → Outdated

* 정렬/필터:

  * 기본 정렬: `last_reported_at` 내림차순
  * 필터:

    * Status별 필터(전체 / Up-to-date / Outdated)
      * Unknown은 “전체”에 포함된다. (옵션으로 Unknown 필터 추가 가능)
    * 텍스트 검색(Workspace ID / Local Path / Repo URL 포함)

#### 5.3.3 UX 요구사항

* Outdated workspace는 행 전체에 약간의 배경색 또는 아이콘을 추가해 눈에 띄게 한다.
* `last_reported_at`는 “YYYY-MM-DD HH:MM (상대 시간: X분/시간 전)” 형태로 표시한다.
* `local_path`는 길 경우 말줄임 처리하되, hover 시 전체 경로를 tooltip으로 표시한다.

#### 5.3.4 빈 상태/에러 처리

* 해당 project에 속한 workspace가 하나도 없으면:

  * “이 project에 등록된 workspace가 없습니다. kkachi workspace register 또는 kkachi init으로 workspace를 등록해 주세요.” 문구 표시.

* `docs_heads`에 해당 project가 없으면:

  * 상단에 경고 배너:

    * “서버 state에 이 project에 대한 docs HEAD 정보가 없습니다. 서버 설정 또는 project 등록을 확인해 주세요.”
  * 테이블은 비우지 않고, 해당 project의 workspace 행을 그대로 보여준다.
    * Docs HEAD 컬럼은 `—`로 표시한다.
    * Status는 모두 Unknown으로 표시한다.

### 5.4 F2-3 전체 상태(raw) 보기 (선택)

#### 5.4.1 목적

* `/state` JSON 구조를 그대로 확인하고 싶은 개발자/운영자를 위한 화면이다.

#### 5.4.2 동작 요구사항

* URL 구조 예시:

  * `/app/debug/state`

* `/api/state` 호출 결과를 prettified JSON으로 화면에 보여준다. (레거시/개발 환경에서는 `/state` fallback 가능)
* 별도 가공 없이 그대로 노출하되, Syntax highlighting 제공(선택).
* 읽기 전용, 편집 불가.

### 5.5 F2-4 에러/로딩/빈 상태

* 공통 로딩 상태:

  * `/api/state` 호출 중에는 스피너 또는 “불러오는 중…” 텍스트 표시.
* 공통 에러 상태:

  * 네트워크 오류, 5xx 응답 등:

    * 화면 상단에 에러 배너 + Retry 버튼.
* 빈 상태:

  * `/api/state`(=`/state`)의 `docs_heads`와 `workspaces`가 모두 비어 있는 경우:

    * “kkachi-server에 아직 project / workspace가 등록되지 않았습니다.” 안내 문구 + v1 CLI 명령 예시(`kkachi project add`, `kkachi init`)를 간단히 보여줄 수 있다.

---

## 6. 서버/API 요구사항 (v2 범위)

### 6.1 사용 API

v2에서는 기존 v1 API만 사용하며, 쓰기 API는 호출하지 않는다.

* `GET /state`

  * 응답 구조 예시는 v1 requirement §5.2 "서버 내부 상태 모델" 및 `/state` 응답 예시를 따른다.
* `GET /docs/head?project=<project>`

  * 필요 시 개별 project HEAD를 별도로 조회하는 용도로 사용할 수 있지만, 기본 설계에서는 `/state`의 `docs_heads`를 우선 사용한다.

### 6.2 신규/변경 API

v2 Web UI를 위해 다음 alias/헬퍼 엔드포인트를 추가한다 (v2 필수).

| 경로           | 메서드 | 설명                               |
| ------------ | --- | -------------------------------- |
| `/api/state` | GET | `/state`의 alias로, 동일 JSON을 반환한다. |
| `/healthz`   | GET | 간단한 헬스체크 JSON을 반환한다.          |

요구사항:

* `/api/state`는 `/state`와 **완전히 동일한 응답 스키마**를 가져야 하며, 서버 내부에서는 핸들러를 재사용한다.
* Web UI에서 사용할 기본 endpoint는 `/api/state`이며, 레거시/개발 환경에서만 `/state` fallback을 허용할 수 있다.
* `/healthz`는 200 OK와 간단한 JSON(예: `{ ok: true }`)을 반환한다.

### 6.3 정적 파일 서빙

* 서버는 v2부터 Web 빌드 결과를 정적 파일로 서빙해야 한다.

요구사항(예시):

* 빌드 결과 위치: `web/dist/`

* 서버 라우팅:

  * `GET /app/*`  → `web/dist/index.html` (SPA 라우팅용)
  * `GET /app/assets/*` → `web/dist/assets/*` (JS/CSS/이미지)
  * `GET /` → `web/dist/index.html` (`/app/`과 동일 SPA 엔트리, 호환용)

* Web UI의 공식 엔트리 URL은 `/app/` 이며, `/app/*` 하위는 모두 SPA 라우트로 처리한다.
* Web 빌드 시 정적 asset 경로가 `/app/assets/...`로 생성되도록 프론트 설정(Vite `base: "/app/"` 등)을 적용한다.

---

## 7. Web UI 기술 스택 및 구조

### 7.1 기술 스택

* Framework: React
* 언어: TypeScript
* 번들러: Vite
* 스타일링: 자유 (초기 버전은 CSS Module 또는 간단한 CSS-in-JS 중 하나 선택)
* 상태 관리:

  * v2 범위에서는 React Query 또는 간단한 custom hook 기반 fetch + local state만 사용
  * Redux 등 복잡한 상태 관리 라이브러리는 도입하지 않는다.

### 7.2 코드 구조 (예시)

```text
web/src/
  api/
    client.ts         # axios/fetch wrapper
    kkachi.ts         # /api/state, /docs/head 호출 함수
  pages/
    ProjectsPage.tsx  # F2-1
    ProjectDetailPage.tsx # F2-2
    RawStatePage.tsx  # F2-3 (선택)
  components/
    Layout/
    ProjectCard.tsx
    WorkspaceTable.tsx
    StatusBadge.tsx
  router/
    index.tsx         # react-router 설정
  App.tsx
  main.tsx
```

요구사항:

* API 호출 로직은 `api/` 디렉토리로 분리하여, 컴포넌트에서 직접 fetch를 호출하지 않는다.
* 에러/로딩 상태 처리는 공통 hook 또는 컴포넌트로 재사용한다.

---

## 8. 비기능 요구사항

### 8.1 성능

* `/api/state` 호출 및 JSON 파싱은 수 밀리초~수백 밀리초 수준의 응답을 목표로 한다.
* 최초 로딩 시 **1회의 `/api/state` 호출**만으로 프로젝트/워크스페이스 화면 모두를 렌더링할 수 있어야 한다.

  * 프로젝트 상세 화면 전환 시 굳이 다시 호출하지 않고, 메모리 캐시를 재사용하는 것을 기본으로 한다.

### 8.2 보안

* v2에서는 별도의 인증·인가를 두지 않는다.
* 모든 HTTP 호출은 동일 Origin(kkachi-server)으로만 수행된다.
* CORS는 기본적으로 비활성(또는 same-origin 허용) 상태를 유지한다.

### 8.3 에러 처리 / 로깅

* 브라우저 콘솔에 에러 전체 스택을 남기되, 사용자 화면에는 간단한 메시지와 Retry 버튼만 제공한다.
* HTTP status 코드별 처리:

  * 4xx (예: `unknown_project` 등의 케이스)는 “서버 설정/등록 문제”로 사용자에게 안내.
  * 5xx / 네트워크 에러는 “서버 오류 또는 네트워크 문제”로 안내.

### 8.4 테스트

* Unit Test

  * 상태 계산 로직 (Up-to-date / Outdated 판별)
  * API client (mock fetch) 응답 처리
* Component Test

  * ProjectsPage / ProjectDetailPage가 mock `/api/state` 응답(또는 동일 스키마의 `/state` fixture)을 기반으로 올바른 UI를 렌더링하는지 검증
* E2E (선택)

  * Playwright 또는 Cypress를 통해:

    * 서버에 fixture `/api/state` 응답을 주입하고(또는 네트워크 인터셉트), Web UI에서 프로젝트/워크스페이스 상태가 올바르게 표시되는지 확인

---

## 9. 향후 확장 고려사항 (v2 설계 시점에서의 제약)

v2는 이후 v3~v9 확장의 기반이 되므로, 다음을 고려하여 설계한다.

* v3에서 Web Terminal(PTY + xterm.js)를 추가하기 쉬운 라우팅 구조 유지 (`/app/terminal` 등).
* v4 이후 Task/Agent 화면 확장을 위해, 프로젝트 상세 화면에 향후 “Tasks” 탭을 추가할 수 있는 레이아웃 사용.
* v2는 same-origin 고정(보안/운영 단순화)이며, 설정은 `/api` 같은 path prefix 수준만 허용한다.
  * full URL/별도 도메인 분리는 v3+에서 별도 요구사항으로 다룬다.
