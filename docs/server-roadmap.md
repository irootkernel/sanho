# Kkachi Server Roadmap

본 문서는 kkachi-server의 서버 개발을 위한 로드맵을 정의한다.
v1 기준 server 설계의 단일 기준 문서이며, 과거 초안/중간 버전은 이 문서를 기준으로 통합·정리되었다.
모든 개발자는 이 문서를 기준으로 기능별로 Clean Architecture를 적용하고,
PR 범위를 엄격히 관리하며, 필수 테스트를 수행해야 한다.

---

## 1. 공통 원칙

### 1.1 아키텍처 원칙

- 레이어 구조
  - `domain`: 엔티티, 값 객체, 추상 Repository 인터페이스
  - `usecase` (application): 유즈케이스, 비즈니스 흐름
  - `interface/http`: HTTP handler, DTO, router, OpenAPI
  - `infra/*`: Git, 파일, 상태 저장소 등의 구현체
- 의존 방향
  - `domain` → 어느 것도 의존하지 않음
  - `usecase` → `domain`만 의존
  - `interface/http` → `usecase`와 DTO만 의존 (domain 타입은 DTO 변환 단에서만 사용)
  - `infra/*` → `domain` 인터페이스 구현, 외부 시스템 의존

### 1.2 개발 순서 원칙 (기능별 수직 슬라이스)

각 기능은 다음 순서로 구현한다.

1. **Domain**

   - 해당 기능에 필요한 entity / value / repository 인터페이스만 정의
2. **Interface (REST + OpenAPI)**

   - HTTP endpoint, DTO, handler skeleton, OpenAPI 스펙
3. **Usecase**

   - domain Repository 인터페이스를 사용하는 유즈케이스 구현
4. **Data (Infra)**

   - 실제 구현체(Git, 파일 등)와 DI wiring
5. **테스트**

   - Unit → Integration → e2e 순서로 필수 시나리오 검증

### 1.3 Git / docs repo / state 처리 원칙

- 각 docs repo는 **일반 Git repo**로 취급한다.
  - GitHub API, go-github, `gh` CLI는 사용하지 않는다.
  - 반드시 `git` CLI (`os/exec`) 또는 필요 시 go-git과 같은 범용 git 라이브러리를 사용한다.
- kkachi-server는 하나 이상의 docs repo에 대한 **로컬 clone**을 유지하며, 모든 작업을 해당 clone에서 수행한다.
  - `git fetch`, `git rev-parse`, `git archive`, `git commit`, `git push` 등을 사용
- 서버 내부 state와 설정의 기본 모델은 다음과 같다.
  - `docs_repos`: `docs_repo_id` → `{ id, path, current_head, repo_url }`
  - `project_to_docs_repo`: `project` → `docs_repo_id`
  - Workspace state에는 `project`, `docs_repo_id`, `local_path`, `repo_url`, `docs_hash`, `last_reported_at`, `owner_email`, `last_actor_email` 등을 저장한다.
- v1 기준으로 Requirement §5.2 및 DECISION D10 에서 정의한 것처럼, 이 state 는 **단일 JSON 파일 기반 `StateRepository`** 로 관리한다.
  - 프로세스 기동 시 JSON 을 읽어 메모리 state 를 초기화하고, 변경 시 전체 state 를 직렬화해 원자적으로 flush 한다.
  - `WorkspaceRepository`, `DocsRepoConfigRepository` 등은 모두 이 단일 state 의 서로 다른 부분에 대한 adapter 역할을 한다.
- 서버 설정 파일에는 최소한 `"project" → "docs_repo_id"` 매핑과 각 `docs_repo_id`에 대한 로컬 clone 경로가 정의되어야 하며,
  Git 관련 infra는 이 매핑과 `docs_repos` 정보를 이용해 올바른 repo에서 작업해야 한다.  
  v1 에서 설정 파일은 **최초 기동 시 state 를 초기화하는 seed/bootstrapping 용도**로 사용되며, 실제 운영 중 project ↔ docs repo 매핑과 docs repo 목록의 단일 소스 오브 트루스는 서버 내부 JSON state (`project_to_docs_repo`, `docs_repos`) 이다. runtime 변경은 `/projects` API 를 통해서만 수행하는 것을 기본 규칙으로 한다.

### 1.4 PR 정책

- PR 본문 코드(테스트 제외) 100줄 미만을 목표로 한다.
- 가능하면
  - Domain, Interface, Usecase, Data 단계를 각각 별 PR로 나눈다.
  - 테스트 코드는 해당 단계 PR에 같이 넣되, 줄 수 계산에서는 제외한다.

### 1.5 테스트 전략

- Unit Test
  - Domain 로직, Usecase 로직
  - 외부 의존성 없이 fake / stub repository 사용
- Integration Test
  - HTTP handler + fake repository 또는 실제 infra
  - `httptest.Server` 사용
- E2E Test
  - 실제 git repo(임시 디렉토리) + file state를 이용해 서버 전체 플로우 검증
- 각 기능마다 “반드시 통과해야 할 테스트 시나리오”를 Roadmap 내에 명시한다.

> 이 문서에서 `sudal`, `sudal_docs` 는 **예시 project / docs repo 이름**일 뿐이며, kkachi-server 는 `sudal`, `dolgorae` 등 여러 project 와 각 project 에 매핑된 docs repo 를 동일한 방식으로 관리한다. 서버 동작 규약은 특정 repo 이름이 아니라 “project” / “docs repo” / `docs_repo_id` 용어를 기준으로 정의한다.

---

## 2. API Endpoint 요약

kkachi-server v1에서 제공하는 주요 endpoint는 아래와 같다.
Phase 번호는 이후 섹션에서 설명하는 구현 순서와 매핑된다.

### 2.1 핵심 API (kkachi CLI에서 사용하는 엔드포인트)

| ID | Endpoint               | Method | 설명                                                                                | 구현 Phase |
| -- | ---------------------- | ------ | --------------------------------------------------------------------------------- | -------- |
| F1 | `/docs/head`           | GET    | 지정된 project에 매핑된 docs repo(main)의 현재 HEAD commit hash 조회 (`project` query 필수)        | Phase 1  |
| F2 | `/workspaces/register` | POST   | workspace 정보를 등록/갱신하고, 현재 해당 project의 docs repo HEAD를 함께 반환                    | Phase 2  |
| F3 | `/docs/snapshot`       | GET    | 지정된 project/docs repo에 대해 특정 commit(또는 HEAD) 기준 docs 디렉토리 snapshot(base64 tar.gz) 제공 (`project` query, `commit` optional) | Phase 3  |

> `/docs/head`와 `/docs/snapshot` endpoint는 모두 `project` query param을 요구하며, server usecase에는 `ProjectName`으로 전달된다.

| F4 | `/docs/push`           | POST   | workspace의 docs snapshot을 기반으로 해당 project의 docs repo 업데이트 시도 (updated / nochange / outdated) | Phase 4  |
| F5 | `/state`               | GET    | project 별 docs HEAD 및 모든 workspace의 상태를 조회하는 디버그용 endpoint                       | Phase 5  |

### 2.2 보조 API (운영/문서용 엔드포인트)

| ID | Endpoint                          | Method | 설명                                        | 구현 Phase                                                  |
| -- | --------------------------------- | ------ | ----------------------------------------- | --------------------------------------------------------- |
| S0 | `/healthz`                        | GET    | 서버 liveness 체크                            | Phase 0                                                   |
| S1 | `/openapi.yaml` / `/openapi.json` | GET    | OpenAPI 스펙 문서를 정적 서빙                      | 각 기능 Interface 단계에서 점진 업데이트 (최소 skeleton은 Phase 1~2 중 시작) |
| S2 | `/docs` 또는 `/swagger`             | GET    | Swagger UI 또는 ReDoc으로 OpenAPI 문서 렌더링 (v1 기본 제공) | Phase 1~2 (문서/검증 편의용)                              |
| S3 | `/projects/{project}`             | DELETE | 지정된 project의 docs repo 설정 및 로컬 clone 디렉토리 제거 | Phase 0~1 (설정/정리 기능)                                |
| S4 | `/workspaces/{workspace_id}`      | DELETE | 특정 workspace(디렉토리) 등록 정보 제거. 로컬 디스크는 건드리지 않음 | Phase 2 (workspace 관리)                                   |
| S5 | `/projects`                       | POST   | project → docs repo 매핑 추가/갱신               | Phase 1~2 (설정 관리)                                     |

서버 개발 시, 각 Phase에서 **해당 Phase에 속한 endpoint 외에는 건드리지 않는 것**을 원칙으로 한다.
예를 들어 Phase 1에서는 `/docs/head`와 관련된 부분만 구현하고, `/docs/push` 관련 타입/로직은 다음 Phase로 남겨 둔다.

---

## 3. Phase 0 – 공통 기반 구축

Phase 0에서는 이후 기능별 구현을 위한 최소한의 프로젝트 뼈대만 만든다.
업무 로직은 전혀 들어가지 않는다.

### P0-1. Go Module & 디렉토리 구조

- 목표
  kkachi-server 프로젝트 기본 구조를 생성한다.
  - 개발 방향
  - 다음 디렉토리만 생성하고, 내용은 최소화한다.
    - `cmd/server`
    - `internal/domain`
    - `internal/usecase`
    - `internal/interface/http`
    - `internal/infra/git`
    - `internal/infra/state`
    - `internal/config`
- Scope
  - 포함
    - `go.mod`, `go.sum` 초기 설정
    - 빈 패키지/파일 혹은 최소한의 컴파일 가능한 코드
  - 제외
    - 어떤 기능도 구현하지 않는다.
    - 모든 REST endpoint, usecase, Git 연동 코드는 **다음 Phase**에서 처리한다.
- 주의할 점
  - 패키지 이름과 디렉토리 구조만 확정하고, 로직을 미리 넣지 않는다.
- 필수 테스트
  - `go test ./...` 이 성공하는지 (빌드 확인)

---

### P0-2. 최소 HTTP 서버 + `/healthz`

- 목표
  이후 모든 기능을 실험할 수 있는 HTTP 서버 실행 기반을 마련한다.
- 개발 방향
  - `cmd/server/main.go`
    - 포트 설정 (env 혹은 기본값 5789)
    - router 초기화
    - `/healthz` GET → 200 OK, 간단 JSON 응답
  - `internal/interface/http/server.go` (또는 유사 파일)
    - `NewHTTPServer` 함수 정의
- Scope
  - 포함
    - 서버 기동
    - `/healthz` endpoint
  - 제외
    - `/docs/*`, `/workspaces/*`, `/state` 등 비즈니스 엔드포인트
    - OpenAPI 서빙
- 주의할 점
  - `/healthz`는 infra 성격이 강하므로, usecase 없이 바로 handler에서 응답해도 무방하다.
- 필수 테스트
  - Integration
    - `httptest`로 서버를 띄우고 `/healthz` 호출 → 200 OK, expected JSON 확인

---

### P0-3. Docs Repo Manager (초기 clone + fetch)

- 목표  
  서버 기동 시 모든 docs_repo가 존재하고 최신 상태인지 보장한다.
- 개발 방향
  - 설정된 모든 docs_repo 경로를 확인하고,
    - 디렉토리가 없으면 `git clone` 을 수행하고,
    - 디렉토리가 있으면 **기동 시 1회만** `git fetch` 를 수행해 최신 상태로 맞춘다.
  - v1에서는 주기적 background fetch(interval 기반 goroutine)는 사용하지 않는다.
    이후 Phase의 `/docs/push` 구현에서 fetch/reset 을 포함하므로, 기능적 correctness 는 push 흐름이 보장한다.
  - 이후 Phase에서 Git repository를 사용할 때는 이 Manager가 생성/동기화한 로컬 clone을 전제로 한다.
  - `/projects`(POST) endpoint 를 통해 runtime 에 새 docs repo 가 추가되거나 기존 매핑이 갱신될 때도,
    Docs Repo Manager 가 해당 정보를 state/config 에 반영한 뒤 동일한 규칙(없으면 clone, 있으면 fetch)으로 로컬 clone 을 준비·동기화해야 한다.  
    이 endpoint 와의 실제 연동(wiring)은 `/projects` 가 도입되는 Phase 1~2에서 완료하고, Phase 0에서는 Manager 인터페이스/헬퍼 수준까지만 정의해도 된다.
- Scope
  - 포함: 초기 clone, 기동 시 1회 fetch
  - 제외: push 흐름과 충돌 처리 로직(F4 이후), 주기 fetch 스케줄링
- 필수 테스트
  - Integration: 빈 디렉토리 → clone, 기존 repo → fetch 호출 여부 검증

---

### P0-4. 프로젝트 제거 Endpoint 스켈레톤

- 목표  
  더 이상 필요 없는 project를 제거하고, 로컬 docs repo clone 디렉토리를 정리할 수 있게 한다.
  - 개발 방향
  - `DELETE /projects/{project}` handler 추가
    - state 의 `project_to_docs_repo` 에서 project 매핑 제거
    - 해당 project 에 연결된 workspace 가 서버 state 에 남아 있으면 기본적으로 **HTTP 409 Conflict + `{"ok": false, "error": "project_has_workspaces"}`** 로 거부하고,
      쿼리 파라미터 `force=true` 가 있는 경우에만 강제 제거를 허용한다.
    - 삭제 성공 후, 해당 docs repo clone 디렉토리가 더 이상 어떤 project 에서도 사용되지 않는다면 로컬 디렉토리를 삭제한다.
  - Docs Repo Manager가 보유한 project/docs_repo 목록과 일관성을 유지하도록 제거 후 리로드/갱신
  - Scope
    - 포함: handler skeleton, state 갱신(`project_to_docs_repo`), 로컬 clone 디렉토리 삭제 로직
    - 제외: cloud storage 삭제 등 확장 기능
    - 참고: workspace 존재 여부 체크 및 409/`force=true` 처리 등 workspace 연동 로직은 `Workspace`/`WorkspaceRepository` 가 도입되는 Phase 2에서 실제 구현을 완료해도 된다.
      Requirement §5.4 및 DECISION D8 에서 정의한 것처럼, v1 최종 상태에서는:
      - 없는 project 는 항상 **HTTP 404 + `{"error": "unknown_project"}`** 를 반환하고,
      - 등록된 workspace 가 남아 있을 때 기본 호출은 **HTTP 409 + `{"error": "project_has_workspaces"}`** 로 거부되며,
      - 위험도가 높은 `force=true` 옵션은 기본 kkachi CLI 에서는 노출하지 않고 curl/별도 관리 도구에서만 사용한다.
- 필수 테스트
  - Integration: 임시 디렉토리에 clone 생성 후 DELETE 호출 → workspace 존재 시 409, `force=true` 시 삭제,
    알 수 없는 project는 404, docs repo 가 더 이상 사용되지 않을 때 로컬 디렉토리 삭제되는지 확인

---

### P1-1. 프로젝트 추가/갱신 Endpoint

- 목표  
  runtime에 project → docs repo 매핑을 추가하거나 갱신할 수 있게 한다.
- 개발 방향
  - `POST /projects` 요청 바디 예:

    ```json
    {
      "project": "sudal",
      "docs_repo_id": "sudal_docs",
      "docs_repo_url": "git@github.com:SeventeenthEarth/sudal_docs.git",
      "actor_email": "karl@example.com"
    }
    ```

  - Docs Repo Manager에 새 repo가 필요한 경우 clone/fetch 트리거
  - 기존 매핑이 있을 때는 갱신 flag를 요구하거나 idempotent 동작 정의
- Scope
  - 포함: handler, state 갱신, 새 docs repo clone 트리거, idempotent 처리
  - 제외: 인증/인가 상세, 복잡한 검증 로직
- 필수 테스트
  - Integration: 신규 project 추가 시 state 저장, clone 호출; 동일 project 재요청 시 충돌/갱신 동작 검증

---

### P2-5. Workspace 제거 Endpoint

- 목표  
  특정 workspace(실제 로컬 디렉토리)를 kkachi-server 상태에서만 제거하며, 디스크는 건드리지 않는다.
- 개발 방향
  - `DELETE /workspaces/{workspace_id}` handler 구현
    - 등록된 workspace를 state에서 삭제
    - `workspace_id`가 존재하지 않으면 404
    - 삭제 후 project 상태를 업데이트 (예: 해당 project의 workspace 카운트, 최종 보고 시간 등)
  - force 옵션은 기본 불필요(디스크 작업이 없으므로), 안전 확인을 위해 id 일치 여부만 검증
- Scope
  - 포함: handler, state 업데이트, 없는 id 처리
  - 제외: 로컬 디스크 삭제, project 삭제 연쇄 처리
- 필수 테스트
  - Integration: workspace 등록 후 DELETE 호출 → state에서 제거되고 재조회 시 없는지 확인

---

## 4. Phase 1 – 기능 1: `GET /docs/head`

이 Phase의 결과로, 개발자는 각 project 에 매핑된 docs repo 의 실제 HEAD hash(예: sudal_docs HEAD)를 반환하는 `/docs/head`를 사용할 수 있어야 한다.

### F1-Domain – Docs HEAD 도메인 정의

- 목표
  docs repo HEAD 조회에 필요한 최소 도메인 모델을 정의한다.
- 개발 방향
  - `internal/domain/docs` 패키지에 다음 추가:
    - `type ProjectName string`
    - `type CommitHash string`
    - `func (h CommitHash) IsZero() bool`
    - `type DocsRepoID string` // Requirement §5.5 에서 정의한 규약에 따라, docs_repo_url 의 repo 이름을 그대로 사용
    - `type DocsReadRepository interface { GetHead(ctx context.Context, project ProjectName) (CommitHash, error) }`
- Scope
  - 포함
    - Project를 식별하는 값 객체(ProjectName)
    - HEAD 값을 표현하는 값 객체(CommitHash)
    - Project별 HEAD를 읽기 위한 추상 repository 인터페이스
  - 제외
    - Snapshot, Push, Outdated 등 모든 쓰기 관련 기능
      → Phase 3, 4에서 확장 예정
    - Workspace 관련 도메인 (`Workspace`, `WorkspaceRepository`)
      → Phase 2에서 정의 예정
- 주의할 점
  - CommitHash는 단순 문자열 래퍼 정도만 구현하고, 과도한 검증 로직을 넣지 않는다.
- 필수 테스트
  - Unit
    - `CommitHash` IsZero 동작 확인
    - 컴파일 확인용 간단 테스트

---

### F1-Interface – `/docs/head` REST + OpenAPI

- 목표
  `/docs/head` HTTP endpoint와 OpenAPI 스펙을 정의한다.
- 개발 방향
  - Request: query param `project` (필수)
  - DTO 정의

    ```go
    type DocsHeadResponse struct {
        Head string `json:"head"`
    }
    ```

  - handler skeleton

    ```go
    type DocsHeadHandler struct {
        UC GetDocsHeadUseCase // 다음 단계에서 구현
    }

    func (h *DocsHeadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
        // 이 단계에서는 query param에서 project를 읽어 UC.Execute(ctx, project) 호출 + 응답 변환 정도의 stub만 유지
    }
    ```

  - router에 `/docs/head` GET 등록
  - `openapi.yaml` 에 `/docs/head` path 추가
- Scope
  - 포함
    - HTTP path, method, response JSON 구조 정의
    - OpenAPI 스펙에 계약 추가
  - 제외
    - 실제 HEAD 계산 로직
    - Git 연동
- 주의할 점
  - handler 내부에 비즈니스 로직을 넣지 않고, usecase 호출에만 집중한다.
  - 요청된 `project` 에 대해 서버 설정/상태 내에 project → docs repo 매핑이 존재하지 않는 경우, HTTP 400(Bad Request) 과 `{"error": "unknown_project"}` 형식의 에러 JSON 을 반환하는 것을 표준으로 한다.
- 필수 테스트
  - Integration
    - fake usecase를 주입하여 `/docs/head` 호출 시 JSON 스키마가 맞는지 확인
    - 등록되지 않은 project 로 호출했을 때 400 및 에러 메시지 반환 여부 확인

---

### F1-Usecase – GetDocsHeadUseCase 구현

- 목표
  DocsReadRepository를 사용해 project별 HEAD를 가져오는 usecase를 구현한다.
- 개발 방향
  - `internal/usecase/docs/get_head.go`

    ```go
    type GetDocsHeadUseCase interface {
        Execute(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error)
    }

    type getDocsHeadUseCase struct {
        docsRepo domain.DocsReadRepository
    }

    func (u *getDocsHeadUseCase) Execute(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error) {
        return u.docsRepo.GetHead(ctx, project)
    }
    ```

  - handler에서 usecase 호출하여 응답 작성
- Scope
  - 포함
    - usecase 구현
    - handler에 usecase 주입 및 호출
  - 제외
    - docsRepo 실제 구현
    - logging, metrics 등 부가 기능
- 주의할 점
  - usecase는 순수하게 domain 인터페이스만 사용해야 하며, infra 타입은 등장하지 않는다.
- 필수 테스트
  - Unit
    - fake DocsReadRepository로 HEAD를 주입하고, usecase가 이를 그대로 반환하는지 테스트
  - Integration
    - fake usecase를 handler에 주입하여 `/docs/head` 응답 JSON이 맞는지 테스트

---

### F1-Data – GitDocsRepository.GetHead 구현 및 Wiring

- 목표
  각 project 에 매핑된 docs repo clone(예: sudal_docs)에서 HEAD hash를 읽어 반환한다.
- 개발 방향
  - infra 레벨 Git client 인터페이스 정의

    ```go
    type GitClient interface {
        RevParseHead(ctx context.Context, repoPath string) (string, error)
    }
    ```

  - GitClient CLI 구현
    - `git -C <repoPath> rev-parse HEAD` 호출
  - `GitDocsRepository` 구현

    ```go
    type GitDocsRepository struct {
        git           GitClient
        projectToRepo map[domain.ProjectName]domain.DocsRepoID
        repoPathByID  map[domain.DocsRepoID]string
    }

    func (r *GitDocsRepository) GetHead(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error) {
        repoID, ok := r.projectToRepo[project]
        if !ok {
            // 도메인 레벨에서 ErrUnknownProject 와 같은 에러로 감싼 뒤,
            // HTTP handler 에서 Requirement §5.8 규약에 따라
            // HTTP 400 + {"error": "unknown_project"} 로 매핑한다.
            return "", ErrUnknownProject
        }
        path, ok := r.repoPathByID[repoID]
        if !ok {
            // 설정/state 불일치이므로, 내부 서버 에러로 처리한다.
            return "", fmt.Errorf("docs repo not configured for id=%s", repoID)
        }
        s, err := r.git.RevParseHead(ctx, path)
        if err != nil {
            return "", err
        }
        return domain.CommitHash(s), nil
    }
    ```

  - `main.go`에서 wiring
    - 서버가 유지하는 state 저장소 또는 설정에서 Requirement §5.2 에서 정의한 `docs_repos`, `project_to_docs_repo` 를 로드하고, 이를 이용해 `projectToRepo`, `repoPathByID` 맵을 구성한다.
    - `gitClient := NewGitClientCLI()`
    - `docsRepo := NewGitDocsRepository(gitClient, projectToRepo, repoPathByID)`
    - `uc := NewGetDocsHeadUseCase(docsRepo)`
    - handler에 주입
  - Scope
    - 포함
      - HEAD 조회에 필요한 Git 호출
      - 각 docs repo clone(예: sudal_docs)이 이미 있다고 가정 (초기 clone/fetch는 P0-3 Docs Repo Manager에서 처리)
    - 제외
      - `git clone`, `git fetch` 등 repo 초기화/동기화 로직
        → `/docs/push` 구현 시점 또는 별도 infra Phase에서 처리 예정
- 주의할 점
  - Git 오류 메시지는 handler에서 적절히 숨기거나 변환할 수 있도록, error wrapping을 해둔다.
- 필수 테스트
  - Integration
    - 임시 디렉토리에 `git init` + commit 하나 생성 후, GitDocsRepository.GetHead 호출 → commit hash 일치하는지 확인
  - E2E
    - 서버를 실제 docs 테스트 repo(예: sudal_docs)와 연결해 띄운 뒤 `/docs/head` 호출 → HEAD hash 확인

---

## 5. Phase 2 – 기능 2: `POST /workspaces/register`

이 Phase의 결과로, kkachi CLI가 서버에 workspace를 등록하고, 서버가 해당 workspace와 연결된 docs repo HEAD 를 기억할 수 있어야 한다.

### F2-Domain – Workspace 엔티티 및 Repository

- 목표
  workspace 정보를 도메인 레벨에서 표현하고, 저장소 인터페이스를 정의한다.
- 개발 방향
  - `internal/domain/workspace` 패키지에 다음 정의:
    - `WorkspaceID` 값 객체 (`ProjectName`은 `domain/docs`에서 정의한 타입을 재사용)
    - `Workspace` struct
      - `ID`, `Project`, `DocsRepoID`, `LocalPath`, `RepoURL`, `DocsHash`, `LastReportedAt`, `OwnerEmail`, `LastActorEmail`
    - `WorkspaceRepository` 인터페이스
      - `Save(ctx context.Context, ws *Workspace) error`
      - `Get(ctx context.Context, id WorkspaceID) (*Workspace, error)`
      - `List(ctx context.Context) ([]*Workspace, error)`는 `/state` 구현 시점(Phase 5)에서 사용 예정
- Scope
  - 포함
    - Workspace 핵심 속성
  - 제외
    - `.kkachi.json`, CLI 설정 로직
      → kkachi CLI에서 처리 예정
- 주의할 점
  - `WorkspaceID` 생성 규칙은 도메인이 아니라 usecase에서 결정한다.
- 필수 테스트
  - Unit
    - Workspace 생성 시 필수 필드 검증 로직(있는 경우) 테스트

---

### F2-Interface – `/workspaces/register` REST + OpenAPI

- 목표
  workspace 등록/갱신 endpoint 계약을 정의한다.
  - 개발 방향
    - Request DTO

      ```go
      type RegisterWorkspaceRequest struct {
          Project    string `json:"project"`
          LocalPath  string `json:"local_path"`
          RepoURL    string `json:"repo_url"`
          ActorEmail string `json:"actor_email"`
      }
      ```

    - Response DTO

      ```go
      type RegisterWorkspaceResponse struct {
          WorkspaceID     string `json:"workspace_id"`
          CurrentDocsHead string `json:"current_docs_head"`
      }
      ```

  - handler skeleton: `POST /workspaces/register`
  - OpenAPI에 path, request/response 스키마 정의
- Scope
  - 포함
    - HTTP 계약 (JSON 필드, 이름, 타입)
  - 제외
    - WorkspaceID 생성 규칙
    - docs repo HEAD 조회 로직
- 주의할 점
  - handler는 usecase 호출 + DTO 변환에 집중하고, 로직을 넣지 않는다.
  - 요청된 `project`에 대해 서버 설정/상태 내에 project → docs repo 매핑이 **존재하지 않을 경우**, HTTP 400(BadRequest) 와 함께 `{"error": "unknown_project"}` 형식의 JSON 을 반환한다.
- 필수 테스트
  - Integration
    - fake usecase 사용 시 200/201 응답과 JSON 스키마가 맞는지 확인
    - 등록되지 않은 project 로 호출했을 때 400 및 에러 메시지 반환 여부 확인

---

### F2-Usecase – RegisterWorkspaceUseCase

- 목표
  클라이언트 요청을 Workspace 도메인 객체로 변환하고, 저장 후 해당 project 의 docs repo HEAD 정보를 응답에 포함한다.
- 개발 방향
  - `RegisterWorkspaceUseCase` 정의
    - 입력: project, localPath, repoURL
    - 의존성:
      - `DocsReadRepository` (HEAD 조회)
      - `WorkspaceRepository`
      - `DocsRepoConfigRepository` 또는 동등한 helper (`project_to_docs_repo` 를 통해 `DocsRepoID` 조회)
  - 동작:

      1. `WorkspaceID` 생성 규칙 적용 (예: `project:localPath`)
      2. `project_to_docs_repo[project]` 를 이용해 `DocsRepoID` 조회 (없으면 도메인 레벨에서 `ErrUnknownProject` 와 같은 에러로 표현하고, HTTP handler 에서 Requirement §5.8 규약에 따라 HTTP 400 + `{"error": "unknown_project"}` 로 매핑)
      3. `DocsReadRepository.GetHead(ctx, project)`로 해당 project docs repo의 현재 HEAD(`head`)를 조회
      4. `WorkspaceRepository.Get` 를 통해 동일 `WorkspaceID` 가 이미 존재하는지 확인한다.
         - 존재하지 않으면: `DocsHash = head`, `LastReportedAt = now`, `DocsRepoID = 조회한 docs_repo_id`, `OwnerEmail = ActorEmail`, `LastActorEmail = ActorEmail` 로 채운 새 `Workspace` 를 생성한다.
         - 이미 존재하면: 기존 `Workspace` 의 `OwnerEmail` 은 그대로 유지하고, `DocsHash`, `LastReportedAt`, `DocsRepoID`, `LastActorEmail` 을 위 규칙에 따라 갱신한다.
      5. 갱신된 `Workspace` 를 `WorkspaceRepository.Save` 로 저장
      6. 응답의 `current_docs_head` 필드에 `head` 값을 넣어 반환
  - handler에서 usecase 호출 결과 → DTO로 변환
- Scope
  - 포함
    - Workspace 저장
    - 현재 docs repo HEAD 반환
  - 제외
    - Workspace 중복 정책(덮어쓰기 vs 에러)은 단순 정책으로 시작하고, 필요 시 추후 강화
- 주의할 점
  - `WorkspaceID` 생성 규칙은 한 곳(usecase)에서만 정의하고, 다른 코드에서 직접 조립하지 않는다.
  - CLI 가 기존 `docs/` 를 재사용해 `.kkachi_docs_hash` 를 과거 hash 로 초기화하는 경우가 있어도, 서버는 `docs_hash` 를 항상 현재 docs repo HEAD 로 저장한다. 이 초기 불일치는 첫 `/docs/push` 성공 시 자동으로 다시 맞춰진다.
- 필수 테스트
  - Unit
    - fake WorkspaceRepo + fake DocsRepo 사용
    - 정상 등록 시 Save/ GetHead 호출 여부 검증
  - Integration
    - fake repos를 주입한 서버에서 `/workspaces/register` 호출해 응답 JSON 검증

---

### F2-Data – FileWorkspaceRepository 구현

- 목표
  워크스페이스 상태를 로컬 JSON 파일로 유지한다.
- 개발 방향
  - `internal/infra/state/file_workspace_repository.go` 구현:
    - in-memory map + JSON 파일 sync
    - `Save`, `Get` 구현 (Save 시 `last_reported_at` 필드를 현재 시각으로 갱신하고, `docs_repo_id`, `owner_email`, `last_actor_email` 등을 Requirement §5.2 의 state 스키마에 맞게 JSON 에 반영)
    - 이후 Phase 5에서 `List` 추가 예정
  - main wiring
    - config에서 `STATE_FILE_PATH` 읽기
    - `workspaceRepo := NewFileWorkspaceRepository(STATE_FILE_PATH)`
    - `RegisterWorkspaceUseCase`에 주입
- Scope
  - 포함
    - 워크스페이스 저장/조회
  - 제외
    - `/state`용 전체 상태 응답 로직
      → Phase 5에서 구현
- 주의할 점
  - 동시 쓰기 시 간단한 Mutex 보호만 우선 적용
- 필수 테스트
  - Integration
    - temp 파일을 사용한 Save/Get 동작 검증
  - E2E
    - 서버를 띄우고 `/workspaces/register` 호출 후 상태 파일 내용 확인

---

## 6. Phase 3 – 기능 3: `GET /docs/snapshot`

이 Phase의 결과로, CLI는 특정 project 에 매핑된 docs repo 의 지정된 commit(또는 HEAD) 기준 docs snapshot(base64 tar.gz)을 내려받을 수 있다.

### F3-Domain – Snapshot 도메인 모델

- 목표
  docs snapshot을 표현하는 타입과 repository 메서드를 추가한다.
- 개발 방향
  - `DocsSnapshot` 정의

    ```go
    type DocsSnapshot []byte
    ```

  - `DocsReadRepository` 인터페이스에 메서드 추가

    ```go
    GetSnapshot(ctx context.Context, project ProjectName, commit CommitHash) (DocsSnapshot, error)
    ```

- Scope
  - 포함
    - Snapshot 도메인 정의, 인터페이스 시그니처
  - 제외
    - tar.gz 포맷 구체 구조는 infra에서 처리
- 주의할 점
  - F1에서 정의한 인터페이스를 변경하므로, 기존 구현(GitDocsRepository)에 stub를 먼저 추가해 컴파일이 깨지지 않도록 한다.
- 필수 테스트
  - Unit
    - 인터페이스/타입 변경에 대한 최소 테스트

---

### F3-Interface – `/docs/snapshot` REST + OpenAPI

- 목표
  project별 snapshot 조회 endpoint를 정의한다.
- 개발 방향
  - Request: query param `project` (필수), `commit` (optional)
  - Response DTO

    ```go
    type DocsSnapshotResponse struct {
        Commit   string `json:"commit"`
        Snapshot string `json:"snapshot"` // base64-encoded tar.gz
    }
    ```

  - handler skeleton
    - commit 파라미터 파싱 + usecase 호출
  - OpenAPI에 `/docs/snapshot` 정의
- Scope
  - 포함
    - JSON 포맷, base64 string 응답
  - 제외
    - raw `application/gzip` 응답 (필요 시 추후 확장)
- 주의할 점
  - commit 미지정 시 HEAD 사용 규칙은 usecase에서 처리한다.
  - v1 기준 `kkachi init` 은 이 endpoint 를 사용해 **각 project 의 현재 HEAD 기준 docs snapshot** 을 받아 로컬 `docs/` 디렉토리를 처음 생성한다.  
    init 이후에는 pre-commit / fix / push 흐름이 snapshot 을 사용해 docs 를 유지·갱신한다.
- 필수 테스트
  - Integration
    - fake usecase 사용 시 commit 지정/미지정 케이스의 JSON 스키마 테스트

---

### F3-Usecase – GetDocsSnapshotUseCase

- 목표
  commit 미지정 시 해당 project의 HEAD를 사용하고, snapshot을 반환하는 로직을 구현한다.
- 개발 방향
  - `GetDocsSnapshotUseCase` 구현
    - `commit`이 비었으면 `DocsReadRepository.GetHead(ctx, project)` 호출
    - 최종 commit에 대해 `DocsReadRepository.GetSnapshot(ctx, project, commit)` 호출
- Scope
  - 포함
    - HEAD fallback 로직
  - 제외
    - snapshot 내용/구조 검증 (test나 client 측)
- 주의할 점
  - 에러 메시지는 handler에서 HTTP status와 매핑하기 쉽도록 계층적으로 감싼다.
  - 존재하지 않는 project 에 대해 snapshot 을 요청한 경우, `/docs/head` 와 동일하게 HTTP 400(Bad Request) 과 `{"error": "unknown_project"}` 형식의 에러 JSON 을 반환한다.
- 필수 테스트
  - Unit
    - commit empty → HEAD 사용
    - commit 지정 → HEAD 사용 없이 snapshot 바로 요청

---

### F3-Data – GitDocsRepository.GetSnapshot 구현

- 목표
  실제 Git repo에서 commit 기준 docs/ 디렉토리를 tar.gz로 묶어 반환한다.
- 개발 방향
  - `GitClient`에 `ArchiveDocs(commit, repoPath) ([]byte, error)` 추가
    - 내부에서 `git -C <repoPath> archive --format=tar <commit> docs/ | gzip`
  - `GitDocsRepository.GetSnapshot` 구현
    - F1에서 정의한 `repoPath map[ProjectName]string`을 사용해 project별 repoPath를 선택한 뒤, `GitClient.ArchiveDocs`를 호출한다.
- Scope
  - 포함
    - docs repo **루트 전체**를 포함한 tar.gz (코드·설정·문서 등)
  - 제외
    - tar 내부 루트를 `docs/` 로 강제하는 이전 규약
- 주의할 점
  - snapshot tar 의 경로들은 항상 docs repo 루트를 기준으로 한 상대 경로이며, Requirement 6.1.1 에서 정의한 것처럼
    로컬 workspace 의 `docs_dir` 값과 관계 없이 서버는 각 project 에 매핑된 docs repo **루트 전체를 docs 트리**로 간주해 내용을 반영한다.
  - v1에서는 대용량 docs에 대한 성능/메모리 최적화는 과도하게 고려하지 않는다.
  - `commit` 파라미터가 docs repo 에 존재하지 않는 commit 을 가리키는 경우, Requirement §6.3.3 의 규약에 따라 **HTTP 400 Bad Request** 와 `{"error": "unknown_docs_commit"}` 형식의 에러 JSON 을 반환한다. 이 에러는 도메인 레벨에서 별도 에러 타입(예: `ErrUnknownDocsCommit`) 으로 표현하고, HTTP handler 에서 일관되게 매핑한다.
- 필수 테스트
  - Integration
    - 샘플 repo에 docs 파일을 만들고 snapshot을 받은 뒤, tar.gz를 풀어 내용이 일치하는지 확인
  - E2E
    - 서버의 `/docs/snapshot` 호출 후 base64 → tar.gz를 디코드하여 검증

---

## 7. Phase 4 – 기능 4: `POST /docs/push`

이 Phase는 kkachi 전체 기능의 핵심이다.
snapshot을 기반으로 새 docs repo commit을 만들고 main 브랜치에 push한다.

### F4-Domain – Push 모델, enum, Repository 인터페이스

- 목표
  push 결과 상태를 표현하는 도메인 모델과 인터페이스를 정의한다.
- 개발 방향
  - Push status enum

    ```go
    type DocsPushStatus string

    const (
        DocsPushStatusUpdated  DocsPushStatus = "updated"
        DocsPushStatusNoChange DocsPushStatus = "nochange"
        DocsPushStatusOutdated DocsPushStatus = "outdated"
    )
    ```

  - Result 모델

    ```go
    type DocsPushResult struct {
        Status      DocsPushStatus
        NewHead     *CommitHash // updated일 때만
        CurrentHead CommitHash  // nochange/outdated일 때
    }
    ```

  - 쓰기용 repository 인터페이스

    ```go
    type DocsWriteRepository interface {
        PushSnapshot(ctx context.Context, project ProjectName, base CommitHash, snapshot DocsSnapshot, actorEmail string) (DocsPushResult, error)
    }
    ```

  - `WorkspaceRepository`에 `UpdateDocsHash(ctx, id, newHash, actorEmail)` 추가 (hash와 `last_reported_at`, 마지막 actor email 등을 함께 갱신)
- Scope
  - 포함
    - updated / nochange / outdated enum과 결과 모델
  - 제외
    - 3-way merge, conflict marker 삽입 로직
      → kkachi CLI에서 처리 예정, 서버는 outdated만 응답
- 주의할 점
  - DocsPushStatus 문자열 값은 `/docs/push` 응답 JSON과 정확히 매칭되어야 한다.
  - 여기서 정의하는 updated / nochange / outdated 및 WorkspaceRepository.UpdateDocsHash 호출 규칙은 Requirements 5.2에서 설명하는 서버 state 모델(`docs_hash` 갱신 규칙)과 정확히 일치해야 한다.
- 필수 테스트
  - Unit
    - enum/string 변환 테스트

---

### F4-Interface – `/docs/push` REST + OpenAPI

- 목표
  push endpoint 계약을 정의한다.
- 개발 방향
  - Request DTO

    ```go
    type DocsPushRequest struct {
        WorkspaceID   string `json:"workspace_id"`
        BaseDocsHash  string `json:"base_docs_hash"`
        DocsSnapshot  string `json:"docs_snapshot"`  // base64 tar.gz
        ActorEmail    string `json:"actor_email"`    // Git user email, audit 용
    }
    ```

  - Response DTO

    ```go
    type DocsPushResponse struct {
        Ok              bool   `json:"ok"`
        Status          string `json:"status"`
        NewDocsHash     string `json:"new_docs_hash,omitempty"`
        CurrentDocsHash string `json:"current_docs_hash,omitempty"`
        Error           string `json:"error,omitempty"`
    }
    ```

  - handler skeleton
    - base64 decode → usecase에 전달하는 부분만 구현
  - OpenAPI에 path, request, response 추가
- Scope
  - 포함
    - JSON 계약
  - 제외
    - push 결과 로직, outdated 판단 로직
      → Usecase/Data 단계에서 구현
- 주의할 점
  - status 문자열 값(`"updated"`, `"nochange"`, `"outdated"`)은 고정하고, 바꾸지 않는다.
  - 요청된 `workspace_id` 가 서버 state 에 존재하지 않는 경우(삭제되었거나 잘못된 id 등)에는 Requirement §5.8 규약에 따라 HTTP 400(BadRequest) 또는 404(Not Found) 중 하나를 선택해 일관되게 사용해야 한다. v1 에서는 단순성을 위해 **400 + `{"error": "unknown_workspace"}`** 형식의 에러 JSON 을 표준으로 사용하며, `DELETE /workspaces/{workspace_id}` 의 404 케이스에서는 **404 + `{"error": "unknown_workspace"}`** 를 사용한다.
  - 동일 docs repo 에 대한 다른 `/docs/push` 가 이미 진행 중인 경우, Requirement §5.9 및 DECISION D5 의 규약에 따라 HTTP 409 와 함께 `{"ok": false, "error": "docs_repo_busy"}` 형식의 에러 JSON 을 반환한다.
  - base_docs_hash 가 서버 docs repo 에 존재하지 않는 commit 을 가리키는 경우에는 Requirement §6.3.3 의 규약에 따라 HTTP 400 과 `{"error": "unknown_docs_commit"}` 를 반환한다.
- 필수 테스트
  - Integration
    - fake usecase 응답에 따라 JSON 응답 필드가 올바르게 매핑되는지 확인

---

### F4-Usecase – PushDocsUseCase

- 목표
  push 비즈니스 로직을 구현한다.
- 개발 방향
  - 입력: workspaceID, baseDocsHash, snapshot, actorEmail
  - 의존성:
    - `WorkspaceRepository`
    - `DocsWriteRepository`
    - `DocsRepoMutexManager` (Requirement §5.9 에서 정의한 `docs_repo_id` 단위 mutex 관리용)
  - 흐름:

    1. `WorkspaceRepository.Get`로 workspace 존재 확인
    2. workspace.Project 를 이용해 `DocsRepoID` 를 얻는다 (도메인 `Workspace` 에 이미 포함되어 있는 값을 사용하거나, 필요 시 `project_to_docs_repo` 를 통해 다시 조회).
    3. `DocsRepoMutexManager.TryLock(docsRepoID)` 를 호출해 **짧은 시간 동안만** lock 을 시도한다.
       - lock 을 획득하지 못하면 `"docs_repo_busy"` 도메인 에러를 반환하고, handler 에서 HTTP 409 + `{"ok": false, "error": "docs_repo_busy"}` 로 매핑한다 (Requirement §5.9).
    4. lock 을 획득한 경우, lock 범위 안에서만 다음을 수행한다.
       - `DocsWriteRepository.PushSnapshot(ctx, workspace.Project, baseDocsHash, snapshot, actorEmail)` 호출
         - Data 레이어 구현에서는 필요 시 commit 메시지에 actor 를 남기거나 audit 로그에 기록할 수 있다.
    5. lock 을 해제한 뒤, 결과 status에 따라 `WorkspaceRepository.UpdateDocsHash` 호출

       - `updated` 인 경우: `NewHead` 값을 사용해 docs_hash, last_reported_at, `last_actor_email` 등을 갱신
       - `nochange` 인 경우: `CurrentHead` 값을 사용해 docs_hash, last_reported_at, `last_actor_email` 등을 갱신
       - `outdated` 인 경우: **push는 실패하지만**, `CurrentHead`(서버 기준 최신 HEAD)를 docs_hash로 반영해
         서버가 기억하는 workspace 기준 hash와 docs repo HEAD를 맞춰 둔다. 이 때도 해당 요청의 actor_email 을 workspace 의 마지막 actor(`last_actor_email`) 로 기록한다.
    6. 결과 status에 따라 handler용 result 구조 생성
- Scope
  - 포함
    - updated/nochange/outdated 상태 흐름
  - 제외
    - Git 세부 에러 처리 (Data 레이어에서 구현)
- 주의할 점
  - usecase 내부에서 Git이나 파일 시스템을 직접 호출하지 않는다.
- 필수 테스트
  - Unit
    - fake DocsWriteRepository + fake WorkspaceRepository 사용
    - updated: `UpdateDocsHash`가 `NewHead`로 호출되는지 검증
    - nochange: `UpdateDocsHash`가 `CurrentHead`로 호출되는지 검증
    - outdated: `UpdateDocsHash`가 `CurrentHead`로 호출되는지 검증 (docs_hash와 last_reported_at 모두 갱신)

---

### F4-Data – GitDocsRepository.PushSnapshot, mutex 등

- 목표
  실제 Git 명령을 사용해 snapshot을 기반으로 새 commit과 push를 수행한다.
- 개발 방향
  - `GitClient` 확장
    - `Fetch`, `CheckoutMain`, `ResetHardToOriginMain`, `ApplySnapshotToDocs`, `DiffIsEmpty`, `Commit`, `Push`
  - `GitDocsRepository.PushSnapshot` 구현
    - `project` 파라미터를 이용해 F1-Data 에서 정의한 `projectToRepo[project]`로 `docs_repo_id` 를 구한 뒤, `repoPathByID[docs_repo_id]` 로 실제 로컬 clone 경로를 조회하고, 해당 repo에서 fetch / reset / diff / commit / push 를 수행한다. (구체적인 데이터 구조는 `docs_repos` / `project_to_docs_repo` state 스키마(requirement §5.x)와 동일하게 유지한다.)
    - 동시성 제어 자체는 Requirement §5.9 및 위 F4-Usecase 에서 설명한 것처럼 usecase 레이어의 `DocsRepoMutexManager` 가 담당하며, Data 레이어에서는 **하나의 push 시퀀스**가 들어왔다는 전제 하에 순차적으로 Git 명령을 수행한다.
    - 시퀀스:

      1. `git fetch origin`
      2. `git checkout main`
      3. `git reset --hard origin/main`
      4. `git rev-parse HEAD` → H_head
      5. base != H_head → status=outdated, CurrentHead=H_head
      6. base == H_head이면

         - snapshot을 `docs/`에 풀기
         - `git diff --quiet` → nochange or updated
         - updated이면 `git add docs`, `git commit -m ...`, `git commit -m "<project> docs update by <actorEmail>"`, `git rev-parse HEAD` → H_new, `git push origin main`
  - `WorkspaceRepository.UpdateDocsHash` 구현
    - 상태 파일에서 해당 workspace docs_hash와 last_reported_at, `last_actor_email`을 현재 시각과 전달받은 actorEmail 에 맞게 업데이트
- Scope
  - 포함
    - 각 project 에 매핑된 docs repo(main 브랜치)에 대한 commit/push
  - 제외
    - Git 충돌 해결, merge 전략
      → outdated를 응답하고, 이후는 kkachi/개발자가 처리
- 주의할 점
  - push 실패 시 에러를 usecase에 올리고, handler에서 적절한 HTTP status(500/503 등)로 치환한다.
  - baseDocsHash 가 docs repo 에 존재하지 않는 commit 을 가리키는 경우, Requirement §6.3.3 의 규약에 따라 도메인 에러(예: `ErrUnknownDocsCommit`) 로 변환하고 HTTP 400 + `{"error": "unknown_docs_commit"}` 로 매핑한다.
- 필수 테스트
  - Integration
    - temp repo에서 base=HEAD인 snapshot push → updated
    - 동일 snapshot push → nochange
    - base가 오래된 상태 → outdated
  - E2E
    - 두 workspace ID로 순차 push → 두 번째의 `/docs/push` 응답이 outdated 인지 확인
    - 두 workspace 에서 같은 project 에 대한 `/docs/push` 를 동시에(또는 거의 동시에) 호출했을 때,
      - 하나는 정상 updated,
      - 다른 하나는 outdated 로 응답하고,
      - 해당 docs repo HEAD 가 일관된 상태를 유지하는지 확인

---

## 8. Phase 5 – 기능 5: `GET /state`

이 Phase의 결과로, 서버가 관리하는 **각 project 별 docs HEAD와 등록된 workspace 상태**를 한 번에 조회할 수 있다.

### F5-Domain – ServerState (선택적)

- 목표
  필요하다면 ServerState 도메인 모델을 정의한다.
- 개발 방향
  - 단순히 usecase에서 DTO만 만들고 끝낼 수도 있다.
  - 필요 시:

    ```go
    // v1 기준: 서버가 관리하는 각 project 별 docs HEAD 와
    // 전체 workspace 리스트를 함께 노출한다.
    type ServerState struct {
        DocsHeads  map[ProjectName]CommitHash
        Workspaces []WorkspaceSummary
    }
    ```

- Scope
  - 포함
    - 상태 조회를 위한 최소 구조
  - 제외
    - 히스토리, 통계 등 확장 정보
- 주의할 점
  - 도메인 모델 없이 DTO로만 처리해도 충분하면, 과도한 추상화를 만들지 않는다.

---

### F5-Interface – `/state` REST + OpenAPI

- 목표
  상태 조회 endpoint 계약을 정의한다.
- 개발 방향
  - Response DTO

    ```go
    type ServerStateResponse struct {
        DocsHeads  map[string]string  `json:"docs_heads"`
        Workspaces []WorkspaceSummary `json:"workspaces"`
        // v1 기준: project 별 docs HEAD 와 workspace 요약만 제공한다.
        // 내부 state 의 docs_repo 상세(`docs_repos`, `project_to_docs_repo`)는 별도 내부 도구에서 사용한다.
    }

    type WorkspaceSummary struct {
        WorkspaceID    string `json:"workspace_id"`
        Project        string `json:"project"`
        DocsRepoID     string `json:"docs_repo_id"`
        LocalPath      string `json:"local_path"`
        RepoURL        string `json:"repo_url"`
        DocsHash       string `json:"docs_hash"`
        LastReportedAt string `json:"last_reported_at"`
        LastActorEmail string `json:"last_actor_email"`
    }
    ```

  - handler skeleton
  - OpenAPI 업데이트
- Scope
  - 포함
    - project 별 HEAD + workspace 리스트
  - 제외
    - paging, filter, sort 등 고급 기능
- 주의할 점
  - 내부 운영/디버그용이므로, 외부 API처럼 강한 backward compatibility를 강요하지 않는다.
- 필수 테스트
  - Integration
    - fake repos로 head, workspaces를 주입해 JSON 구조를 검증

---

### F5-Usecase – GetStateUseCase

- 목표
  DocsReadRepository와 WorkspaceRepository를 동시에 호출해 상태를 구성한다.
- 개발 방향
  - `GetStateUseCase` 구현
    - `DocsReadRepository.GetHead`
    - `WorkspaceRepository.List` (Phase 2/4에서 구현한 메서드를 활용)
    - `DocsReadRepository.GetHead` 는 서버 설정/상태에 등록된 각 `project` 에 대해 호출하며, 필요한 project 목록은 `project_to_docs_repo` 등에서 별도 repo/헬퍼를 통해 주입받는다.
- Scope
  - 포함
    - 현재 시점의 snapshot 수준 상태
  - 제외
    - 고급 필터링/검색
- 주의할 점
  - 동시 업데이트 상황에서도 “대략적 snapshot”이면 충분하므로, 강한 일관성까지는 요구하지 않는다.
- 필수 테스트
  - Unit
    - fake repos로 HEAD + workspace 리스트 조합이 제대로 응답에 반영되는지 확인

---

### F5-Data – WorkspaceRepository.List 구현

- 목표
  상태 파일에서 모든 workspace를 읽어온다.
- 개발 방향
  - `FileWorkspaceRepository.List` 구현
- Scope
  - 포함
    - 전체 workspace 목록 반환
  - 제외
    - 조건 검색, sort
- 필수 테스트
  - Integration
    - 상태 파일에 여러 workspace 저장 후 List가 동일한 수와 데이터를 반환하는지 확인
  - E2E
    - 서버에서 `/workspaces/register` 를 여러 번 호출한 뒤 `/state` 응답이 이를 반영하는지 확인

---

## 9. Phase 6 – `kkachi clean` 대응 (workspace 해제 강건화)

- 목표  
  CLI의 `kkachi clean`이 서버 workspace 등록을 안전하게 해제할 수 있도록, `DELETE /workspaces/{workspace_id}`(S4) 동작을 재점검하고 idempotent 하게 유지한다.

- 구현/동작 포인트
  - 기존 S4 endpoint를 재사용하되, 동일 workspace에 대한 반복 삭제를 허용한다.
  - 존재하지 않는 workspace일 때는 **404 + `{"error": "unknown_workspace"}`**를 반환하되, 서버 state에 부정합이 없도록 한다. (CLI는 경고 후 계속 진행할 수 있어야 함.)
  - 삭제 시 해당 workspace 엔트리만 제거하고, docs repo clone이나 다른 workspace state에는 영향을 주지 않는다.
  - 상태 파일 flush까지 완료된 것을 성공 조건으로 삼는다.

- 테스트
  - workspace 등록 후 삭제 → state에서 사라지는지 확인.
  - 동일 workspace를 두 번 삭제 → 두 번째는 404 `unknown_workspace`.
  - 다른 project/workspace state가 영향을 받지 않는지 검증.
