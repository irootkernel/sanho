# Kkachi Server v2 Roadmap

## 0. 전제

- v1 기준 kkachi-server와 kkachi CLI, `/state`, `/docs/head` 등 핵심 기능은 이미 구현되어 있다.
- v2는 **읽기 전용 Web 대시보드(kkachi-web)** 를 추가하고, 이를 위해 서버는 최소한의 API alias 및 정적 파일 서빙만 확장한다.
- v2 범위에서는 새로운 쓰기 API를 추가하지 않으며, 기존 v1 워크플로우(kkachi CLI + Git hook)를 그대로 존중한다.
- v2는 인증/인가 없이 접근 가능하다고 가정하되, v3(Web Terminal)부터 옵션 토큰 기반 보호를 붙일 수 있도록 auth middleware를 **비활성 기본값**으로 설계할 여지를 남긴다.

---

## 1. Server Roadmap (kkachi-server v2 범위)

서버는 v1에서 이미 존재하는 상태 모델과 API를 기준으로, Web UI가 요구하는 최소한의 확장을 수행한다.  
기본 원칙은 **기존 API 계약을 깨지 않고 그대로 유지하는 것**이다.

### 1.0 공통 원칙 (Agile Delivery)

- 모든 STASK는 **항상 동작 가능한 서버 바이너리**를 산출하는 단위로 정의한다.
- 각 STASK 안에서 필요한 **테스트 코드 추가/수정**과 **배포 스크립트·문서 업데이트**까지 포함해서 처리한다.
- 별도의 “테스트만 하는 Task”, “배포 문서만 쓰는 Task”는 두지 않고, 각 STASK의 완료 정의(DoD)에 자연스럽게 녹인다.

---

### 1.1 Server Tasks 개요

- **STASK-1**: 상태 모델 및 `/state` / `/docs/head` API 계약 고정 + 관련 단위 테스트
- **STASK-2**: Web용 API alias(`/api/state`) 및 정적 파일 서빙/라우팅 + 통합 테스트
- **STASK-3**: 빌드/배포 파이프라인 정리 및 배포 체크리스트 + 로깅/에러 핸들링

각 STASK는 **테스트와 배포 관점의 완료 기준(DoD)** 를 포함하여 정의한다.

---

### STASK-1. 상태 모델 / API 스펙 고정

**목표**

- v2 Web이 기대하는 `/state` / `/docs/head` 스키마와 동작을 안정적으로 고정한다.
- v2 이후에도 해당 스키마를 기준으로 클라이언트를 진화시킬 수 있도록, breaking change를 방지한다.

**세부 작업**

1. `/state` 응답 스키마 고정

   - 응답이 항상 다음 형태를 만족하는지 확인 및 테스트 추가:

     - `docs_heads: { [project: string]: docs_head_hash }`
     - `workspaces: { workspace_id, project, docs_repo_id, local_path, repo_url, docs_hash, last_reported_at, last_actor_email }[]`
   - v2 Web에서 이 구조를 그대로 가정하므로, 서버에서 임의로 필드명을 변경하거나 제거하지 않는다.

2. `/docs/head` 의 동작 보장

   - `GET /docs/head?project=<project>` 가 항상 `docs_heads[project]`와 일관된 값을 반환하는지 확인.
   - 내부 구현에서 `docs_heads`와 별도 소스 간에 불일치가 생기지 않도록 정합성 점검.

3. 상태 저장 로직 점검

   - 내부 state(JSON 파일 등)에 `docs_repos`, `project_to_docs_repo`, `workspaces` 가 정확히 유지되는지 재점검.
   - 특히 `workspaces[].last_reported_at`, `last_actor_email` 값이 `/state` 응답으로 노출되는지 확인.

**테스트 / Done 기준 (DoD)**

- `/state` 응답 JSON 구조를 검증하는 단위 테스트가 존재한다.
- `/docs/head?project=...` 응답이 `docs_heads[project]`와 일관됨을 검증하는 테스트가 존재한다.
- 실 서버 실행 후 `/state` 응답에서 `last_reported_at`, `last_actor_email` 필드를 실제로 확인할 수 있다.

---

### STASK-2. Web용 API alias 및 정적 서빙

**목표**

- kkachi-web이 same-origin에서 `/api/state`와 정적 자원을 통해 동작하도록 한다.
- SPA 라우팅 및 정적 파일 서빙 규칙을 명확히 하고, Web 대시보드를 안정적으로 제공한다.

**세부 작업**

1. `/api/state` 엔드포인트 구현

   - 메서드: `GET`
   - 동작: 기존 `/state` 핸들러를 그대로 재사용 (동일 JSON 응답).
   - 구현 방식:

     - Router 레벨에서 `/api/state` → `/state` 핸들러로 바인딩.
		   - 요구사항:

		     - `/state` 와 응답 스키마 100% 동일.
		     - Web UI 기본 호출 경로는 `/api/state` 로 **고정**한다. (`/state` fallback 전제 없음)

2. 간단한 헬스체크 API (v2 필수)

	   - `GET /healthz` → `{ ok: true }`
	   - Web에서 직접 사용하지는 않지만, 배포 환경에서 상태 체크용으로 활용한다.

3. Web 빌드 산출물 위치 확정

   - 기본: `kkachi/web/dist/`
   - 빌드 산출: `index.html`, `assets/*` 등.

4. 정적 서빙 핸들러 구현

		   - 라우팅 규칙 예:

			     - `GET /api/*` → API (Web 전용 엔드포인트 prefix)
			     - `GET /assets/*` → `web/dist/assets/*`
			     - `GET /*` → `web/dist/index.html` (SPA 라우팅)
		   - 구현 옵션:

		     - Go `http.FileServer` 로 디렉토리 서빙.
		     - 또는 embed(FS) 활용해 단일 바이너리로 패키징.

5. 404 처리 / SPA fallback

		   - SPA 내부 라우트(`/projects/...`, `/debug/...` 등)로 오는 요청은 항상 `index.html` 로 fallback.
		   - `/api/*` 는 API로, `/assets/*` 는 정적 파일로 구분한다.
		   - `/state`, `/docs/*`, `/healthz` 등 기존 API/정적 라우트는 먼저 매칭되고, 그 외만 SPA fallback 처리한다.

6. CORS / Same-origin 정책 적용

   - Web UI와 API를 동일 Origin에서 제공.
   - 동일 Origin을 전제로 하므로, 별도 CORS 설정은 기본적으로 불필요.

**테스트 / Done 기준 (DoD)**

- 서버 단위/통합 테스트 또는 수동 검증으로 다음을 확인:

	  - `/api/state` 응답이 `/state` 와 완전히 동일하다.
		  - 서버 실행 후 `GET /` 요청 시 `web/dist/index.html` 이 로딩된다.
		  - `GET /projects/...` 요청이 SPA fallback으로 `index.html` 을 반환한다.
		  - `GET /assets/...` 요청이 실제 정적 파일로 응답한다.

---

### STASK-3. 빌드/배포 파이프라인 및 운영

**목표**

- web 빌드 + server 빌드/배포 과정이 문서화/자동화되어, 동일한 절차로 재현 가능하게 만든다.
- 정적 파일이 없거나 잘못 배포된 경우에도 원인을 빠르게 파악할 수 있도록 로깅/에러 메시지를 정리한다.

**세부 작업**

1. Monorepo 빌드 파이프라인 정의

   - 기본 순서:

     1. `cd web && npm install && npm run build`
     2. `go build ./cmd/server`
   - 또는 서버 빌드 시 `web/dist` 존재 여부를 체크하고, 없으면 경고/에러를 출력.

2. 환경 변수 / 설정 값 정의

   - Web 정적 경로 root (예: `WEB_DIST_DIR`).
   - Listen 포트, base URL 등은 v1과 동일하게 유지.

3. 로깅 / 에러 핸들링

   - `/api/state` 및 정적 파일 서빙에 대해 적절한 로그를 남긴다.
   - 정적 파일이 없을 때 500 대신, 명확한 에러 메시지 출력(예: “web/dist 빌드 필요” 등).

4. 배포 문서화

	   - v2 서버 배포 시 체크리스트:

	     - web 빌드 수행 여부 (`web/dist` 생성 확인).
	     - `/api/state` 응답 확인.
	     - `/` 접속 시 SPA 로딩 여부.

**테스트 / Done 기준 (DoD)**

- 단일 커맨드(예: `make server-with-web`) 또는 문서화된 절차로 web + server 빌드를 재현할 수 있다.
- `web/dist` 가 없을 때 서버 로그/에러 메시지에서 원인을 바로 파악할 수 있다.
- 배포 체크리스트를 따라 실제 환경에 올린 뒤, `/`, `/api/state` 가 정상 동작하는 것을 확인했다.
