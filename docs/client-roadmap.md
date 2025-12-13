# Kkachi Web v2 Roadmap (kkachi-web)

## 0. 전제

- v1 기준 kkachi-server 기능 및 상태 모델(`docs_repos`, `project_to_docs_repo`, `workspaces`)이 이미 구현되어 있다.
- v1 기준 kkachi CLI 및 Git hook 기반 워크플로우가 운영 중이며, v2는 이를 변경하지 않는다.
- v2는 **서버의 기존 REST API**(특히 `/state`, `/docs/head`) 위에 구축되는 **읽기 전용 Web UI(kkachi-web)** 를 추가하는 범위이다.
- 배포 환경은 개인 또는 소규모 팀의 로컬 네트워크를 전제로 하며, 인증/인가 없이 접근 가능하다고 가정한다.
- SPA 공식 엔트리는 `/`로 고정한다. 서버는 `/assets/*`를 정적 asset으로 서빙하고, `/api/*` 및 기존 API 경로(`/state`, `/docs/*`, `/healthz` 등)를 제외한 `GET /*`에 대해 `index.html` SPA fallback을 제공하며, `/api/state`와 `/healthz`를 필수로 노출하고, 정적 빌드 미존재 시 친절한 안내를 반환한다.

---

## 1. Client Roadmap (kkachi-web, React+TS+Vite)

Web 쪽은 기능이 많기 때문에, 단계별로 쪼개어 정리한다.  
각 단계는 독립적인 작업 단위로, 우선순위에 따라 점진적으로 도입할 수 있다.

### 1.1 공통 원칙 (Agile Delivery)

- 모든 CTASK는 **브라우저에서 실제로 동작하는 kkachi-web 빌드 산출물**을 목표로 한다.
- 각 CTASK 안에 해당 범위를 검증하는 **테스트 코드(단위/컴포넌트/E2E 중 적절한 수준)** 와 **간단한 배포/빌드 가이드 업데이트**를 포함한다.
- 테스트/배포만을 위한 별도의 Task는 만들지 않고, 각 CTASK의 완료 정의(DoD)에 포함해서 관리한다.

### 1.2 Client Tasks 개요

- **CTASK-1**: kkachi-web 기본 셋업 + `/api/state` 클라이언트 + Debug State 페이지  
  - 내용: `web/` 패키지 생성, React+TS+Vite 템플릿, 라우터/레이아웃 기본 틀, API 타입 정의 및 HTTP 클라이언트, `RawStatePage`(URL: `/debug/state`)를 통해 `/api/state` 응답을 그대로 노출하는 최소 동작 가능한 버전.
- **CTASK-2**: 프로젝트 리스트 대시보드(F2-1) 구현  
  - 내용: 상태 fetch hook, 프로젝트별 집계 로직, `ProjectsPage` UI(테이블/정렬/필터), 기본 로딩·에러·빈 상태 처리까지 포함한 메인 대시보드.
- **CTASK-3**: 프로젝트 상세 화면(F2-2) 구현  
  - 내용: `ProjectDetailPage` 라우팅/데이터 준비, workspace 상태 계산(Up-to-date/Outdated/Unknown), 상세 테이블/필터/검색, `StatusBadge` 및 시간 포맷 유틸 적용.
- **CTASK-4**: 공통 UX/상태 처리 + 비기능 요구사항  
  - 내용: 공통 로딩/에러/빈 상태 컴포넌트 정리, 성능(캐싱/최소 호출), 설정화(`VITE_KKACHI_API_PREFIX`, path prefix 전용), 테스트 보강(상태 계산 유닛 테스트, 주요 페이지 컴포넌트 테스트, 선택적 E2E) 및 향후 라우팅 확장성 정리.

---

### 2-1. 프로젝트 셋업 / 인프라

1. Monorepo 내 `web/` 패키지 생성

   - 구조 예:

     ```text
     kkachi/
       web/
         package.json
         vite.config.ts
         tsconfig.json
         index.html
         src/
           main.tsx
           App.tsx
           router/
           pages/
           components/
           api/
     ```

2. 기본 스택 구성

   - React + TypeScript + Vite 초기 템플릿.
   - ESLint / Prettier / 테스트 프레임워크(Jest or Vitest) 설정.
   - Vite 빌드 `base`는 `/`(기본값)을 유지해 asset URL이 `/assets/...`로 생성되도록 한다. (예: `vite.config.ts`에서 `base: "/"` 또는 설정 생략)

3. 라우터 도입

   - React Router (v6 이상) 설치.
   - `router/index.tsx` 에서 라우트 매핑:

     - `/` → ProjectsPage (F2-1)
     - `/projects/:projectName` → ProjectDetailPage (F2-2)
     - `/debug/state` → RawStatePage (F2-3, optional, dev 전용 혹은 `?debug=1` 로만 노출)

4. 공통 레이아웃 컴포넌트

   - `<Layout>`: 상단 헤더, 좌/상단 navigation, 컨텐츠 영역.
   - 헤더에:

     - “Kkachi Web v2” 타이틀
     - “새로고침” 버튼 (수동 `/api/state` refetch)

---

### 2-2. API 클라이언트 계층

5. API 타입 정의 (`api/kkachi.ts`)

   - `/api/state` 응답 타입: (`/state`와 동일 스키마)

     ```ts
     export interface KkachiState {
       docs_heads: Record<string, string>;
       workspaces: WorkspaceSummary[];
     }

     export interface WorkspaceSummary {
       workspace_id: string;
       project: string;
       docs_repo_id: string;
       local_path: string;
       repo_url: string;
       docs_hash: string;
       last_reported_at: string;
       last_actor_email: string;
     }
     ```

   - 타입 드리프트 방지:

     - v2에서는 수동 정의를 유지하되, `/api/state` 샘플 fixture(JSON)를 두고 파싱 테스트로 계약을 검증한다. (`/state`와 동일 스키마)
     - 이후 Go struct → TS 타입 자동 생성(go2ts 등) 스크립트를 추가할 여지를 남긴다.

6. HTTP 클라이언트 wrapper (`api/client.ts`)

   - fetch 또는 axios 기반:

     - 기본 baseURL: `window.location.origin`.
     - `/api/state` 호출 함수: `getState(): Promise<KkachiState>`.
   - 공통 에러 처리 (에러 객체 통일).

7. 상태 fetch hook 구현

   - 예: `KkachiStateProvider` + `useKkachiState()`:

     - 최상위 Layout/App에서 `/api/state`를 1회 호출해 context에 캐시.
     - 하위 라우트는 context를 통해 `data`, `isLoading`, `error`, `refetch`를 공유.
     - React Query를 도입해도 Provider 내부 구현만 교체하면 되도록 API를 고정한다.

---

### 2-3. 공통 UI 컴포넌트

8. StatusBadge 컴포넌트

   - props: `status: 'up_to_date' | 'outdated' | 'unknown'`.
   - 텍스트 / 색상 매핑:

     - Up-to-date → 녹색.
     - Outdated → 주황/빨강.
     - Unknown → 회색.

9. Time 표시 유틸

   - `formatDateTime(isoString)` → `YYYY-MM-DD HH:MM`.
   - `formatRelativeTime(isoString)` → “X분 전 / X시간 전” 형태.

10. 테이블 컴포넌트

    - `<Table>` / `<TableRow>` / `<TableCell>` 등 or 단순 CSS 테이블.
    - 긴 경로에 대한 tooltip (local_path).

11. ErrorBanner / LoadingSpinner

    - API 에러 공통 배너: 상단에 메시지 + Retry 버튼.
    - 로딩 시 스피너 또는 “불러오는 중…” 텍스트.

---

### 2-4. F2-1 프로젝트 리스트 화면 (Dashboard)

12. 프로젝트별 집계 로직 구현

    - `docs_heads` + `workspaces` 로부터 다음을 계산:

      - project 목록 = `Union(workspaces[].project, docs_heads 키)`.
      - 각 project 대해:

        - `docs_head = docs_heads[project] ?? null`
        - `workspace_count` = 해당 project 의 workspace 개수.
        - `unknown_count` = `docs_head`가 없을 때 해당 project 의 workspace 개수.
        - `outdated_count` = `docs_head`가 있을 때만 `workspace.docs_hash !== docs_head` 인 개수. (`docs_head` 없으면 0)
        - `last_reported_at_max` = 해당 project workspace 들의 `last_reported_at` 최대값.

13. ProjectsPage UI 구현

    - 컬럼:

      - Project
      - Docs HEAD
      - Workspaces (총 개수)
      - Outdated (개수)
      - Last updated (포맷된 `last_reported_at_max`)
	    - 각 row 클릭 시 `/projects/:projectName` 로 이동.

14. 정렬/필터 옵션

    - v2 최소 범위:

      - 기본: project 이름 오름차순.
      - 추가: “Outdated 있는 project 우선 보기” 토글 → `docs_head`가 있는 project 중 outdated_count > 0 인 행을 위로. (`docs_head` 없는 project는 Unknown으로 별도 표기)
    - 다중 정렬·고급 필터는 v2 범위 밖(추가 CTASK로 분리)으로 둔다.

15. 빈 상태/에러 처리

    - `docs_heads` 와 `workspaces` 둘 다 비어 있는 경우는 전역 “완전 빈 state” 처리(2-7 참고).
    - `workspaces.length === 0` 이지만 `docs_heads` 는 존재하는 경우:

      - 프로젝트 목록은 표시하고, 상단 배너로 “아직 workspace가 없습니다. kkachi init 또는 kkachi workspace register를 실행해 주세요.”를 안내한다.
    - API 실패:

      - ErrorBanner + “Retry” 버튼 → `refetch()`.

---

### 2-5. F2-2 프로젝트 상세 화면

16. 라우팅 / 데이터 준비

	    - URL: `/projects/:projectName`.
	    - 진입 시:

	      - 이미 로드된 state 데이터를 재사용 (Context or 상위에서 props 전달).
	      - 없으면 `useKkachiState()` 로 fetch.
    - 필터링:

      - `const projectWorkspaces = workspaces.filter(w => w.project === projectName);`
      - `const docsHead = docs_heads[projectName];`

17. Status 계산 로직

    - 각 workspace 에 대해:

      - if `!docsHead` → status = 'unknown'.
      - else if `workspace.docs_hash === docsHead` → 'up_to_date'.
      - else → 'outdated'.

18. Workspace 테이블 UI

    - 컬럼:

      - Workspace ID
      - Docs Repo ID
      - Local Path (긴 경우 ellipsis + tooltip)
      - Repo URL
      - Docs Hash
      - Docs HEAD (project 기준)
      - Status (StatusBadge)
      - Last Reported (absolute + relative time)
      - Last Actor (last_actor_email)

19. 정렬/필터 기능

    - 기본 정렬: `last_reported_at` 내림차순.
    - v2 최소 필터:

      - Status 필터 (All / Up-to-date / Outdated).
      - 단순 텍스트 검색: workspace_id, local_path, repo_url 대상 substring 검색.
    - 복합 정렬·고급 필터는 후속 범위로 남긴다.

20. UX 디테일

    - Outdated workspace row:

      - 옅은 배경색, 아이콘, 강조.
    - 마지막 갱신 시간:

      - “YYYY-MM-DD HH:MM (X시간 전)” 같이 복합 표기.

21. 빈 상태 / docs_heads 없음 처리

    - workspace는 있는데 `docs_heads[projectName]` 없음:

      - 상단 경고 배너:

        - “이 project에 대한 docs HEAD 정보가 서버 state에 없습니다. 서버 설정 또는 project 등록을 확인해 주세요.”
    - `projectWorkspaces.length === 0`:

      - “이 project에 등록된 workspace가 없습니다. kkachi workspace register 또는 kkachi init으로 workspace를 등록해 주세요.”

---

### 2-6. F2-3 Raw State 화면 (옵션)

22. RawStatePage 구현

	    - URL: `/debug/state`.
	    - `/api/state` 응답을 pretty JSON 으로 그대로 노출. (`/state`와 동일 스키마)
	    - Syntax highlighting (예: prism, highlight.js) 도입 여부는 선택.
	    - 완전 읽기 전용. 운영 노출 여부를 명확히: dev 전용이거나 `?debug=1` 접근만 허용.

---

### 2-7. 공통 로딩/에러/빈 상태 처리

23. Loading 상태

	    - `/api/state` 호출 중:

      - 중앙 스피너 + “불러오는 중…” 텍스트.

24. 에러 상태

    - 네트워크/5xx:

      - ErrorBanner: “서버에 연결할 수 없습니다. 새로고침을 시도하거나 서버 상태를 확인해 주세요.”
    - 4xx:

      - 필요 시 “서버 설정/등록 문제”로 구분된 메시지.

25. 전역 “완전 빈 state” 처리

    - `docs_heads` 와 `workspaces` 둘 다 비어 있는 경우:

      - 안내 메시지:

        - “kkachi-server에 아직 project / workspace가 등록되지 않았습니다.”
        - 아래에 CLI 명령 예시 짧게 노출 (텍스트만).

---

### 2-8. 비기능: 성능 / 보안 / 테스트

26. 성능 요구사항 반영

	    - `/api/state`는 최상위 Provider에서 최초 로딩 시 1회 호출 후 context 캐시.
	    - 프로젝트 상세 화면 이동 시, 기존 데이터를 캐시에서 사용 (리로드 시에만 refetch).

27. 보안 / 네트워크

	    - 동일 Origin만 전제, CORS 설정 불필요.
	    - 브라우저에서 다른 도메인 호출 금지.
	    - 확장 고려: v3(Web Terminal)부터 옵션 토큰 기반 보호가 활성화될 수 있으니, API client 계층에서 (기본 비활성) auth header를 주입할 수 있는 설정 포인트를 남긴다.

28. 유닛 테스트

    - 상태 계산 함수 테스트:

      - project 집계 (workspace_count, outdated_count).
      - workspace status 계산 (Up-to-date/Outdated/Unknown).
    - API client mock 테스트:

      - `/api/state` 응답 파싱 및 에러 처리.

29. 컴포넌트 테스트

    - `ProjectsPage`:

      - mock state 응답을 주입하고, 테이블 렌더링 내용 검증.
    - `ProjectDetailPage`:

      - Outdated/Up-to-date 행 표현, 필터/검색 동작 테스트.

30. E2E 테스트 (선택)

    - Playwright/Cypress:

	      - 서버에 fixture `/api/state` 응답 주입.
      - “단 하나의 happy path” 시나리오만: 리스트 → 상세 페이지 정상 표시 확인.
      - 에러/네트워크 실패 시나리오는 유닛/컴포넌트 테스트로 커버.

---

### 2-9. 향후 확장 고려 (레이아웃/라우팅 관점)

31. 라우팅 구조 확장성 확보

	    - 향후:

	      - `/terminal` (Web Terminal) 추가 가능하도록 라우트 설계.
	      - `/projects/:projectName/tasks` 탭 등 추가 용이한 레이아웃.

32. 설정화

    - API prefix를 `.env` 또는 Vite 환경 변수로 처리(same-origin path prefix 전용):

     - 예: `VITE_KKACHI_API_PREFIX=/api`.
    - v2에서는 full URL을 허용하지 않고 path prefix만 지원한다. 별도 도메인 분리는 후속 CTASK로 분리한다.

---

### 2-10. 서버 연동 / 배포 체크리스트 (v2 필수)

33. SPA 엔트리

	    - 공식 URL: `/` → `index.html` (SPA).

34. `/api/state` (Web 기본 엔드포인트)

	    - 기존 `/state` 핸들러를 그대로 재사용해 `/api/state`에 매핑한다. (Web은 `/api/state`만 호출)

35. `/healthz`

    - 200 OK + 간단 JSON(예: `{ ok: true }`)을 반환하는 헬스 체크 엔드포인트를 노출한다.

36. 정적 빌드 미존재 시 UX

	    - 서버 기동 시 `web/dist` 미존재 로그 경고.
	    - `/` 요청 시 “kkachi-web 빌드가 없습니다. web/ 디렉토리에서 npm run build 후 서버를 재기동하세요.”와 같이 명시적 안내를 반환.

37. 빌드 순서 문서화

    - Monorepo 빌드 순서: `web/`에서 `npm run build` → root에서 `go build` (또는 `make`).
    - README/Makefile 등에 반영하여 CI/로컬 모두 동일하게 맞춘다.
