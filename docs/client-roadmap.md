# Kkachi Web v2 Client Roadmap (kkachi-web)

## 0. 전제 (v2 범위 고정)

- v1 기준 kkachi-server / kkachi CLI / Git hook 워크플로우는 **그대로 유지**한다. (v2 Web은 이를 변경하지 않는다.)
- v2 Web(kkachi-web)은 **읽기 전용(Read-only)** 대시보드이며, Web에서 어떠한 쓰기 작업(`/docs/push` 등)도 수행하지 않는다.
- 서버는 v2에서 다음을 이미 제공한다고 가정한다. (server-roadmap 완료 상태)
  - `GET /api/state` : `GET /state`와 **동일 JSON 스키마**를 반환하는 alias
  - `GET /healthz`
  - 정적 파일 서빙: `GET /assets/*` → `web/dist/assets/*`
  - SPA fallback: `GET /*` (단, `/api/*`, `/assets/*`, 기존 API 경로 제외) → `web/dist/index.html`
- Web UI는 **항상** `/api/state`만 호출한다. (`/state` fallback은 두지 않는다.)
- SPA 공식 엔트리는 `/`로 고정한다. (Vite `base`는 `/` 유지)

---

## 1. v2 Client 목표/범위

요구사항(requirement.md) 기준 v2 Web은 아래 기능을 제공한다.

- **F2-1 (필수)** 프로젝트 리스트(Dashboard)
  - project별 docs HEAD, workspace 수, outdated 수, 마지막 업데이트 시각 요약
- **F2-2 (필수)** 프로젝트 상세
  - 특정 project의 workspace 리스트 + Up-to-date/Outdated/Unknown 상태 표시
  - 필터(상태), 검색, 기본 정렬(last_reported_at desc)
- **F2-3 (선택)** Raw state 보기
  - `/api/state` JSON을 개발자 친화적으로 그대로 표시
- **F2-4 (필수)** 로딩/에러/빈 상태
  - 네트워크 오류, 완전 빈 state(docs_heads/workspaces 모두 비어있음) 등

비범위(명시): v2에서는 Web에서 docs 편집/푸시/프로젝트 변경/워크스페이스 등록 같은 쓰기 기능을 제공하지 않는다.

---

## 2. 작업 방식 (Agile + Top-down + Clean Architecture)

### 2.1 Agile Delivery

- 모든 **CTASK는 “브라우저에서 실제로 동작하는 빌드 산출물”**을 목표로 한다.
- 각 CTASK는 해당 범위를 검증하는 **테스트(유닛/컴포넌트/E2E 중 적절한 수준)** 를 포함한다.
- “테스트만”, “문서만” 같은 별도 Task는 만들지 않고, **각 CTASK의 Done 정의(DoD)** 안에 포함한다.

### 2.2 Top-down 구현 규칙

- 한 화면(또는 사용자 여정)을 **UI부터 먼저** 만들고, 버튼/토글/검색 같은 인터랙션을 먼저 배치한다.
- 기능(데이터/로직)은 **아래 레이어를 점진적으로 채워 넣으며** 붙인다.
- 아랫단이 아직 구현되지 않은 단계에서는, 해당 호출이 **명시적으로 `UnimplementedError`를 발생**시키도록 두고,
  - 테스트는 그 시점에는 “미구현이므로 UnimplementedError가 노출되는 것이 정상”임을 기대한다.
  - 다음 CTASK에서 해당 레이어가 구현되면, 테스트 기대를 “정상 동작”으로 업데이트한다.

### 2.3 Clean Architecture 적용 방식

요구사항이 단순하더라도 v3/v4 확장을 막지 않도록, v2부터 레이어 경계를 고정한다.

- **Domain**
  - 엔티티/값 객체/순수 함수(상태 계산, 집계)만 포함
  - React/HTTP/환경변수 접근 금지
- **Presentation**
  - React UI(페이지/컴포넌트), 라우팅, 사용자 이벤트 처리
  - Application의 usecase/port에만 의존
- **Application**
  - 유스케이스(조회/리프레시), 캐시/스토어, 포트(interfaces)
  - 구현체는 Data에 위임
- **Data**
  - `/api/state` 호출(fetch/axios), DTO↔Domain 매핑, 에러 매핑

의존 방향은 항상 다음만 허용한다.

`presentation → application → domain`

`data → application + domain`

---

## 3. 코드 구조 (제안)

> 실제 폴더명은 팀 컨벤션에 맞게 조정 가능하지만, 레이어 경계는 유지한다.

```text
web/
  src/
    app/
      App.tsx
      main.tsx
      di/
        AppRuntimeProvider.tsx
        createRuntime.ts
    domain/
      errors/
        UnimplementedError.ts
      models/
        KkachiState.ts
        Workspace.ts
        ProjectSummary.ts
        Status.ts
      services/
        computeProjectSummaries.ts
        computeWorkspaceStatus.ts
    application/
      ports/
        KkachiStateRepository.ts
      usecases/
        GetKkachiState.ts
      stores/
        KkachiStateStore.ts
    data/
      http/
        HttpClient.ts
      repositories/
        ApiKkachiStateRepository.ts
    presentation/
      router/
        routes.tsx
      layout/
        Layout.tsx
      components/
        ErrorBanner.tsx
        Loading.tsx
        StatusBadge.tsx
      pages/
        ProjectsPage.tsx
        ProjectDetailPage.tsx
        RawStatePage.tsx
  test/
    fixtures/
      api-state.sample.json
```

### DI(의존성 주입) 기본 규칙

- `AppRuntimeProvider`(또는 유사)에서 **usecase/repository 구현체를 주입**한다.
- 테스트에서는 Provider를 교체해
  - unimplemented 구현체
  - in-memory fake
  - fetch mock 기반 real repository
  를 상황에 맞게 주입한다.

---

## 4. 테스트 전략 (v2 최소 세트)

- **Unit (Domain)**
  - status 계산(Up-to-date/Outdated/Unknown)
  - project 집계 로직(workspace_count, outdated_count, last_reported_at_max 등)
- **Component (Presentation)**
  - ProjectsPage 렌더링/정렬 토글/라우팅
  - ProjectDetailPage 필터/검색/빈 상태/경고 배너
- **API Contract (Data)**
  - `/api/state` 샘플 fixture를 파싱/매핑하는 테스트로 타입 드리프트 방지
- **E2E (선택, 1개 시나리오만)**
  - “Projects → Detail” happy path

### “Unimplemented”를 테스트로 다루는 규칙

- `UnimplementedError`는 Domain(errors)에 정의한다.
- 아직 구현되지 않은 port/usecase/repository의 기본 구현은 `throw new UnimplementedError('...')` 로 통일한다.
- Presentation은 ErrorBoundary(또는 공통 에러 핸들링)를 통해
  - UnimplementedError는 “미구현(개발 중)” 메시지로 표시
  - 네트워크/서버 오류는 “서버 연결 실패” 메시지로 표시
- 해당 기능이 구현되는 CTASK에서 테스트 기대를 “정상 렌더링/정상 인터랙션”으로 갱신한다.

---

## 5. CTASK 작업 순서 (v2)

아래 CTASK는 **순서대로 진행**하는 것을 전제로 한다.
각 CTASK는 항상 “빌드 가능 + 테스트 green” 상태로 종료한다.

### 5.1 CTASK ↔ 요구사항 매핑

| CTASK | 사용자 결과물(요약) | Requirement 매핑 |
|---|---|---|
| CTASK-1 | 라우팅/레이아웃/페이지 뼈대 + Unimplemented 기반 TDD 루프 시작 | F2-4(기본 골격) |
| CTASK-2 | `/api/state` 수집 vertical slice + RawStatePage 실제 동작 | F2-3(+F2-4 일부) |
| CTASK-3 | ProjectsPage(대시보드) 완성 + 집계 로직 | F2-1 |
| CTASK-4 | ProjectDetailPage 완성 + 상태/필터/검색 | F2-2 |
| CTASK-5 | 공통 UX/성능/설정/테스트 보강 + 선택 E2E | F2-4 + 비기능 |

---

## CTASK-1. Client Bootstrap + Clean Architecture Skeleton (Unimplemented-first)

### 목표

- `web/` 패키지를 생성하고, **라우팅/레이아웃/페이지 뼈대**가 동작하는 SPA를 만든다.
- Clean Architecture 레이어 골격을 만들고, 아직 미구현인 하위 레이어는 `UnimplementedError`로 통일한다.

### 범위

**Domain**
- `KkachiState`, `Workspace`, `Status` 등 핵심 타입 정의
- `UnimplementedError` 정의

**Presentation**
- 라우트 고정
  - `/` → ProjectsPage
  - `/projects/:projectName` → ProjectDetailPage
  - `/debug/state` → RawStatePage
- `<Layout>`
  - 상단 타이틀(“Kkachi Web v2”)
  - “새로고침” 버튼(동작은 우선 미구현이어도 버튼/이벤트는 존재)
- 각 페이지는 최소 UI를 렌더링하되, 데이터 의존 부분은 usecase 호출 시 `UnimplementedError`가 발생하도록 둔다.

**Application / Data**
- Port/Usecase/Repository 인터페이스만 정의
- 기본 구현체는 모두 UnimplementedError를 throw

### 테스트

- 라우팅 테스트: 각 URL 진입 시 해당 페이지가 렌더링된다.
- “새로고침” 클릭 시(또는 페이지 로드 시) 미구현 호출이 발생하면, Error UI에 **Unimplemented** 메시지가 표시된다.
  - 이 테스트는 CTASK-2부터 RawStatePage에 대해서는 제거/변경된다.

### DoD

- `npm run dev`, `npm run build`, `npm test`가 모두 성공한다.
- 코드 구조가 레이어 경계를 지킨다(React 코드가 domain/data를 직접 import 하지 않음).

---

## CTASK-2. `/api/state` Vertical Slice + RawStatePage 구현 (F2-3)

### 목표

- 서버의 `GET /api/state`를 실제로 호출해 상태를 가져오고, `/debug/state` 화면에서 pretty JSON으로 보여준다.
- 이후 F2-1/F2-2 구현에 재사용할 **State 조회 유스케이스/스토어**를 확정한다.

### 범위

**Domain**
- `/api/state` 스키마에 맞는 domain 타입 고정
  - `docs_heads: Record<string, string>`
  - `workspaces: Workspace[]`

**Presentation**
- RawStatePage
  - 로딩/에러/성공 상태 UI
  - “새로고침” 버튼이 실제로 refetch를 트리거

**Application**
- `GetKkachiState` 유스케이스 구현
- (권장) `KkachiStateStore` 도입
  - `data`, `isLoading`, `error`, `refresh()`
  - CTASK-3/4에서 재사용

**Data**
- `ApiKkachiStateRepository`
  - `GET {VITE_KKACHI_API_PREFIX}/state` 호출(기본 `/api/state`)
  - 에러 매핑(HTTP status, 네트워크 오류)
- `/api/state` 샘플 fixture 추가(계약 테스트용)

### 테스트

- Data contract test
  - 샘플 fixture가 repository/mapper를 통과해 domain 타입으로 파싱됨
- Component test (RawStatePage)
  - 성공 시 JSON 렌더링
  - 실패 시 ErrorBanner + Retry(새로고침)
- CTASK-1의 “RawStatePage는 Unimplemented” 테스트는 제거하고, 이제 “정상 호출”을 기대하도록 갱신

### DoD

- 로컬 서버 환경에서 `/debug/state`가 실제 상태를 표시한다.
- `/api/state` 외 경로는 호출하지 않는다.
- 테스트 green.

---

## CTASK-3. ProjectsPage 구현 (F2-1) + 집계 로직

### 목표

- 메인 대시보드(`/`)에서 project 리스트 요약을 표시한다.
- 프로젝트별 집계 로직을 Domain으로 고정하고, UI는 이를 단순 렌더링만 한다.

### 범위

**Domain**
- `computeProjectSummaries(state)` 구현
  - project 목록 = `Union(Object.keys(docs_heads), workspaces[].project)`
  - 각 project 요약:
    - `docs_head` (없으면 null)
    - `workspace_count`
    - `unknown_count` (= docs_head 없을 때 해당 project workspaces 수)
    - `outdated_count` (= docs_head 있을 때 docs_hash != docs_head 인 workspaces 수)
    - `last_reported_at_max` (= 해당 project workspaces의 max)

**Presentation**
- ProjectsPage
  - 테이블(또는 카드)로 요약 표시
  - 정렬
    - 기본: project 이름 오름차순
    - 토글: “Outdated 있는 project 우선”
  - row 클릭 → `/projects/:projectName` 이동

**Application**
- CTASK-2에서 만든 store/usecase 재사용

**Data**
- 변경 없음(CTASK-2 구현 재사용)

### 테스트

- Unit test (Domain)
  - 집계 로직(unknown/outdated/last_reported_at_max)
- Component test (ProjectsPage)
  - fixture state 주입 → 테이블 렌더링 검증
  - 토글 동작 시 정렬 변화 검증
  - row 클릭 시 라우팅 이동 검증

> 이 CTASK 종료 시점에는 ProjectDetailPage는 아직 미구현일 수 있다.
> 이 경우 “상세 화면은 UnimplementedError를 표시한다”는 테스트를 유지한다.

### DoD

- `/`에서 project 요약이 정상 표시된다.
- “workspace가 없지만 docs_heads는 존재” 케이스에서 요구사항 문구(안내 배너)가 노출된다.
- 테스트 green.

---

## CTASK-4. ProjectDetailPage 구현 (F2-2) + 상태/필터/검색

### 목표

- 프로젝트 상세(`/projects/:projectName`)에서 workspace 테이블과 상태(Up-to-date/Outdated/Unknown)를 표시한다.
- 최소 필터/검색/정렬 UX를 제공한다.

### 범위

**Domain**
- `computeWorkspaceStatus(workspace, docsHead)` 구현
  - docsHead 없음 → `unknown`
  - 같음 → `up_to_date`
  - 다름 → `outdated`
- 정렬/필터/검색을 위한 순수 함수 구현

**Presentation**
- ProjectDetailPage
  - projectWorkspaces 필터링
  - docsHead 표시(없으면 `—`)
  - 테이블 컬럼 (v2 구현 기준)
      - Local Path, Status, Repo URL, Docs Hash, Last Reported, Last Actor
      - *(Workspace ID, Docs Repo ID, Docs HEAD 컬럼은 v2에서 생략 - 정보가 중복되거나 Local Path로 충분히 식별 가능)*
  - 기본 정렬: `last_reported_at` desc
  - 필터: Status(All / Up-to-date / Outdated)
  - 검색: workspace_id/local_path/repo_url substring
  - Outdated 행 강조
  - docsHead 없음 경고 배너 / workspace 0개 빈 상태 처리

**Application / Data**
- CTASK-2/3 재사용

### 테스트

- Unit test (Domain)
  - status 계산
  - 필터/검색/정렬
- Component test (ProjectDetailPage)
  - outdated/up-to-date/unknown 렌더링
  - status 필터/검색 동작
  - docsHead 누락 경고 배너
  - workspace 0개 빈 상태

### DoD

- F2-2 요구사항을 충족한다.
- 테스트 green.

---

## CTASK-5. 공통 UX/성능/설정 마무리 + 선택 E2E

### 목표

- v2 비기능 요구사항을 충족한다.
  - 최초 로딩 1회 `/api/state` 호출로 주요 화면 렌더링
  - 공통 로딩/에러/빈 상태
  - 환경변수 기반 API prefix
- (선택) 최소 E2E 1개 시나리오로 회귀 안전장치 확보

### 범위

**Presentation**
- 공통 컴포넌트 정리
  - `Loading`, `ErrorBanner`, `EmptyState`, `StatusBadge`
- 전역 “완전 빈 state”(docs_heads/workspaces 모두 비어있음) UX
  - “kkachi-server에 아직 project/workspace가 등록되지 않았습니다.” + CLI 안내 문구
- 날짜/시간 표기 유틸
  - `YYYY-MM-DD HH:MM` + 상대 시간(“X분 전 / X시간 전”)

**Application**
- App-level 캐시 정책 확정
  - App 진입 시 store가 1회 fetch
  - 라우트 이동은 캐시 재사용
  - “새로고침” 버튼만 refetch

**Data**
- `VITE_KKACHI_API_PREFIX` 지원(기본값 `/api`)
  - same-origin path prefix만 허용 (full URL 비허용)

**Test**
- (선택) Playwright/Cypress E2E
  - “Projects → Detail” happy path
  - 네트워크는 fixture 또는 인터셉트

### DoD

- “최초 1회 호출 + 캐시 재사용”이 동작한다.
- `/`, `/projects/:projectName`, `/debug/state` direct URL 진입(새로고침)에도 정상 렌더링된다.
- 테스트 green.

---

## 6. 배포/운영 메모 (v2)

- 빌드
  - `cd web && npm ci && npm run build`
  - 산출물: `web/dist/`
- 서버는 `web/dist`를 정적 서빙한다. (서버 미구현/미배포 시 `/`에서 친절한 안내 필요)
- Web은 인증/인가를 전제로 하지 않는다(로컬 네트워크). 다만 v3부터 토큰 인증 옵션이 붙을 수 있으므로,
  - API client는 헤더 주입 포인트를 남겨두되 v2에서는 기본 비활성으로 둔다.
