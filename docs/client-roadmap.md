# Kkachi Client Roadmap

본 문서는 kkachi CLI 및 Git hook 개발을 위한 로드맵을 정의한다.  
모든 개발자는 이 문서를 기준으로 기능별로 Clean Architecture를 적용하고,  
PR 범위를 엄격히 관리하며, 필수 테스트를 수행해야 한다.

---

## 1. 공통 원칙

### 1.1 아키텍처 원칙

- 레이어 구조
  - `domain`
    - Workspace, DocsVersion, DocsStatus, HookContext 등 도메인 개념
    - 충돌 마커 검사 규칙, docs 상태 판단 규칙
  - `usecase` (application)
    - `InitWorkspace`, `ShowStatus`, `PreCommitHook`, `FixDocs`, `PrePushHook`, `CommitMsgHook` 등 유즈케이스
  - `interface/cli`
    - CLI 프레임워크(cobra 등)를 이용한 커맨드 정의
    - 인자 파싱, exit code, 사람이 읽는 출력 포맷
  - `infra/http`
    - kkachi-server REST API client (`/docs/head`, `/workspaces/register`, `/docs/snapshot`, `/docs/push`, `/state`)
  - `infra/fs`
    - `.kkachi.json`, docs_hash_file(기본: `.kkachi_docs_hash`), `.kkachi_pending_fix` 파일 읽기·쓰기
    - docs 디렉토리 스캔, tar.gz 생성, base64 인코딩
  - `infra/git`
    - `git diff`, `git rev-parse`, staged 변경 감지 등 로컬 Git 연동
    - 필요 시만 사용하고, 가능한 한 domain/usecase 층에 직접 노출하지 않는다.
- 의존 방향
  - `domain` → 어느 것도 의존하지 않음
  - `usecase` → `domain`만 의존
  - `interface/cli` → `usecase`와 DTO·view model만 의존 (infra 타입은 usecase 안으로 숨긴다)
  - `infra/*` → `domain` 인터페이스 구현, 외부 시스템(http, 파일, git)에 의존

### 1.2 개발 순서 원칙 (기능별 수직 슬라이스)

각 기능은 다음 순서로 구현한다.

1. Domain
   - 해당 기능에 필요한 entity / value / repository 인터페이스 정의
2. Usecase
   - 순수한 비즈니스 흐름 구현
   - 외부 의존성은 모두 interface로 추상화
3. Interface (CLI)
   - subcommand, flag, 인자 파싱, 출력 포맷
4. Infra (HTTP, FS, Git)
   - 실제 구현체 작성 및 main에서 wiring
5. 테스트
   - Unit → Integration → e2e 순서로 필수 시나리오 검증

이때 가능한 한 **기능별 수직 슬라이스**로 PR을 쪼갠다.  
예를 들어 `kkachi init`에 대해서 Domain + Usecase + 최소 CLI + fake infra만 먼저 올리고,  
HTTP·FS 구현과 e2e는 후속 PR로 나눈다.

### 1.3 kkachi-server 연동 원칙

- CLI는 kkachi-server REST API만 사용한다.
- 사용 대상 endpoint
  - `GET /docs/head` (`project` query param 필수)
  - `POST /workspaces/register`
  - `GET /docs/snapshot` (`project` query param 필수, `commit` optional)
  - `POST /docs/push`
  - `GET /state`
  - `POST /projects`
  - `DELETE /projects/{project}`
  - `DELETE /workspaces/{workspace_id}`
- `/projects` 및 `DELETE /projects/{project}` 는 **설정/정리용 커맨드**(`kkachi project add/delete`)에서 사용하며, 일반적인 init·commit 워크플로우는 주로 현재 디렉토리 기준 `kkachi init` → `/workspaces/register`, `/docs/*`, `/state`에 의존한다.
  - HTTP client는
  - 타임아웃을 반드시 설정한다.
  - 서버 에러 메시지를 그대로 노출하지 않고, CLI용 에러 메시지로 변환한다.
    - 특히 `400/404` 등으로 `{"error": "unknown_project"}` 또는 `{"error": "unknown_workspace"}` 와 같은 오류 코드가 반환되는 경우, 이를 "서버에 등록되지 않은 project/workspace입니다. 'kkachi init' 또는 관리용 커맨드(`kkachi project add`, `kkachi workspace register`)를 다시 실행해 주세요." 와 같은 메시지로 변환하고, 해당 명령은 exit code 1로 종료한다.
- 서버의 enum 값(status: `updated`, `nochange`, `outdated`)과 hash 표현은 서버와 완전히 일치해야 한다.
- CLI는 `/docs/head`, `/docs/snapshot` 호출 시 항상 `.kkachi.json`의 `project` 값을 query param으로 전달해야 한다.

### 1.4 Git / docs 디렉토리 처리 원칙

- docs 디렉토리
  - `.kkachi.json` 에서 `docs_dir` 값을 읽어 사용하되, 기본값은 `docs`
  - tar.gz snapshot 생성 시 루트 디렉토리는 항상 `docs/` 로 고정한다.
- 충돌 마커 검사 규칙
  - 한 파일 안에 아래 세 토큰이 모두 존재하면 “충돌 마커 존재”로 본다.
    - `<<<<<<<`
    - `=======`
    - `>>>>>>>`
  - pre-commit / pre-push / fix에서 공통 유틸로 사용한다.
- Git diff
  - docs 변경 여부 판단은 infra/git wrapper를 통해 `git diff` 를 호출해서 수행한다.
    - pre-commit: "현재 commit에 포함될 docs 변경"을 기준으로 검사
    - commit-msg: staged 변경을 기준으로 검사

### 1.5 PR 정책

- PR 본문 코드(테스트 제외) 100줄 미만을 목표로 한다.
- 가능하면 다음과 같이 쪼갠다.
  - 공통 infra (HTTP client, FS util 등)
  - `init` 수직 슬라이스
  - `status` 및 read-only hook 수직 슬라이스
  - `pre-commit` / `commit-msg` 수직 슬라이스
  - `fix` / `pre-push` 수직 슬라이스
- 테스트 코드는 해당 기능 PR에 포함하되, 코드 줄 수 계산에서는 제외한다.

### 1.6 테스트 전략

- Unit Test
  - Domain 로직 (DocsStatus 계산, conflict marker detector 등)
  - Usecase 로직 (fake infra 사용)
- Integration Test
  - 실제 CLI entry를 `testing`에서 호출
  - fake HTTP server, temp 디렉토리 기반 FS로 외부 의존성을 대체
- E2E Test
  - 실제 kkachi-server 테스트 인스턴스 + temp Git repo 사용
  - 대표 워크플로우(정상 commit, outdated 발생, fix, pre-push 차단)를 전체 플로우로 검증

> 이 문서에서 `sudal`, `sudal_app`, `sudal_docs`는 **예시 project 그룹 이름**일 뿐이며, kkachi는 `sudal`, `dolgorae` 등 여러 project와 각각의 docs repo에 대해 동일한 방식으로 동작한다. CLI 동작 규약은 특정 repo 이름이 아니라 “project” / “docs repo” 용어를 기준으로 정의한다.

---

## 2. CLI 기능 요약

### 2.1 서브커맨드 목록

| ID | Command                        | 설명                                                         | 서버 API 사용 | 구현 Phase |
|----|--------------------------------|--------------------------------------------------------------|--------------|-----------|
| C0 | `kkachi version`               | 현재 kkachi CLI 버전 출력                                   | 없음         | Phase 0   |
| C1 | `kkachi init`                  | workspace 초기화, project 등록, workspace 등록, 로컬 설정 및 hook 설치 | `/projects`, `/workspaces/register`, `/docs/snapshot` | Phase 2   |
| C2 | `kkachi status`                | docs 기준 버전과 서버 HEAD, pending fix 상태 표시            | `/docs/head?project` | Phase 2   |
| C3 | `kkachi fix`                   | outdated 병합 이후 충돌 해결 완료 후, 최종 docs를 서버에 반영 | `/docs/head?project`, `/docs/push` | Phase 5   |
| C4 | `kkachi project add`           | 명시된 project와 docs repo를 kkachi-server에 등록/갱신       | `POST /projects` | Phase 2   |
| C5 | `kkachi workspace register`    | 명시된 workspace 디렉토리를 kkachi-server에 등록            | `POST /workspaces/register` | Phase 2   |
| C6 | `kkachi workspace unregister`  | 명시된 workspace 등록 정보를 서버에서 제거                  | `DELETE /workspaces/{workspace_id}` | Phase 2   |
| C7 | `kkachi project delete`        | project 자체를 서버에서 제거, 디스크는 건드리지 않음 | `DELETE /projects/{project}` | Phase 2~3* |
| C8 | `kkachi state`                 | 서버에 등록된 docs HEAD 및 workspace 상태 조회 (기본: 현재 project만, `--all` 사용 시 전체 project) | `GET /state` | Phase 5   |
| H1 | `kkachi hook pre-commit`       | commit 직전 docs sync 및 outdated 처리, conflict 삽입        | `/docs/push`, `/docs/snapshot?project` | Phase 4   |
| H2 | `kkachi hook post-checkout`    | checkout 직후 상태 표시                                      | `/docs/head?project` | Phase 3   |
| H3 | `kkachi hook post-merge`       | merge 이후 상태 표시                                         | `/docs/head?project` | Phase 3   |
| H4 | `kkachi hook post-rewrite`     | rebase 등 rewrite 이후 상태 표시                             | `/docs/head?project` | Phase 3   |
| H5 | `kkachi hook pre-push`         | push 직전 conflict·pending fix 체크                          | 없음         | Phase 5   |
| H6 | `kkachi hook commit-msg`       | docs 변경 커밋의 commit 메시지에 `docs-version` 태그 추가    | 없음         | Phase 4   |

### 2.2 Git hook 역할 재정리

| Hook 이름      | 호출 시점                          | kkachi CLI 역할 요약                                         |
|----------------|-------------------------------------|-------------------------------------------------------------|
| pre-commit     | commit 직전                        | docs 변경 감지, conflict 검사, `/docs/push` 또는 outdated 흐름 처리, commit 차단 여부 결정 |
| post-checkout  | 브랜치 또는 commit checkout 직후   | `kkachi status` 실행, 상태만 출력, exit 0 유지              |
| post-merge     | `git merge` 또는 merge 기반 pull 이후 | `kkachi status` 실행, 상태만 출력, exit 0 유지              |
| post-rewrite   | rebase 등 rewrite 완료 직후        | rebase 인 경우 `kkachi status` 실행                         |
| pre-push       | 원격 push 직전                     | docs conflict 및 `.kkachi_pending_fix` 검사, 필요시 push 차단 |
| commit-msg     | commit 메시지 확정 직전            | docs 변경 commit에 `docs-version: <hash>` 추가               |

---

## 3. Phase 0 - 공통 기반 구축

### P0-1. Go Module 및 디렉토리 구조

- 목표  
  kkachi CLI 프로젝트 기본 구조를 생성한다.

- 개발 방향
  - 디렉토리 구조
    - `cmd/kkachi`
    - `internal/domain`
    - `internal/usecase`
    - `internal/interface/cli`
    - `internal/infra/http`
    - `internal/infra/fs`
    - `internal/infra/git`
    - `internal/config`
  - `cmd/kkachi/main.go`
    - root command 정의
    - subcommand skeleton 등록(`init`, `status`, `fix`, `hook` 등)
- Scope
  - 포함
    - `go.mod` 초기 설정
    - 최소한의 build 가능 코드
  - 제외
    - 실제 비즈니스 로직
    - 서버 연동
- 필수 테스트
  - `go test ./...` 빌드 통과
  - `kkachi help` 실행 시 usage 출력 확인

---

### P0-2. 기본 CLI 환경 및 `version` 커맨드

- 목표  
  이후 기능 개발을 위하여 최소 level의 CLI 경험을 제공한다.

- 개발 방향
  - `kkachi version`
    - 빌드 타임 변수(`Version`, `Commit`, `BuildDate`) 출력
  - 공통 로깅 유틸
    - `--verbose` 플래그 처리
    - debug 로그 출력 여부 제어
  - exit code 정책 정의
    - 0: 정상 종료
    - 1: 사용자가 인지하고 조치해야 하는 오류(환경 문제, 설정 오류, 서버/네트워크 통신 오류 등)로 인해 **현재 작업(commit/push/명령)이 차단된 상태**
    - 2 이상: 예상하지 못한 내부 버그나 치명적 오류(스택 트레이스 등과 함께 보고용으로만 사용, 일반 워크플로우에서는 등장하지 않도록 한다)
- 필수 테스트
  - Unit
    - version 문자열 포맷 테스트
  - Integration
    - `kkachi version` 실행 결과 parse

---

## 4. Phase 1 - 공통 infra 및 domain

Phase 1은 이후 모든 기능에서 재사용할 공통 모듈을 구현한다.

### P1-1. Domain 타입 정의

- 목표  
  workspace, docs 버전, 상태 표현을 domain 레벨에서 정의한다.

- 개발 방향
  - `WorkspaceID`, `ProjectName`, `DocsHash`, `DocsStatus` 등 value object 정의

    ```go
    type WorkspaceID string
    type ProjectName string
    type DocsHash string

    type DocsStatus string

    const (
        DocsStatusUnknown   DocsStatus = "unknown"
        DocsStatusUpToDate  DocsStatus = "up_to_date"
        DocsStatusOutdated  DocsStatus = "outdated"
    )
    ```

  - `WorkspaceConfig` 도메인 모델
    - serverUrl, workspaceID, project, **actorEmail**, docsDir, docsHashFile, pendingFixFile
  - `DocsRepoID` 계산 규칙
    - `domain` 계층에 `type DocsRepoID string` 을 정의하고, `docs_repo_url` 에서 Git URL 의 repo 이름을 그대로 사용해 `docs_repo_id` 를 계산하는 규칙은
      `kkachi init`(P2-1) 과 `kkachi project add`(P2-5) 등에서 **공통 helper 함수**로 재사용한다.
  - `DocsStatusUnknown` 사용 규칙
    - 서버와의 통신 오류 등으로 `/docs/head` 를 호출할 수 없어 HEAD 비교 자체를 수행하지 못한 경우,
      내부적으로 `DocsStatusUnknown` 을 사용할 수 있다.
    - 기본적인 CLI UX 에서는 이 상황을 “status: unknown (서버에 연결할 수 없습니다)” 와 같은 메시지 + exit code 1 로 표현하고,
      read-only hook(post-checkout, post-merge, post-rewrite) 에서는 Git 동작을 막지 않기 위해 경고 로그만 남기고 exit code 0 을 유지한다.
- 필수 테스트
  - Unit
    - zero value 판단, string 변환 등 간단 로직 테스트

---

### P1-2. `.kkachi.json` Config Loader

- 목표  
  모든 명령에서 공통으로 사용하는 workspace 설정 로딩을 구현한다.

- 개발 방향
  - `infra/fs` 패키지에 config loader 구현
    - 현재 working directory에서 `.kkachi.json` 탐색
    - JSON decode 후 domain `WorkspaceConfig` 로 변환
  - usecase 레벨에 `LoadConfig` 인터페이스 정의
    - 각 유즈케이스는 loader를 통해 config를 얻고, 없으면 사용자 친화적 에러 반환
- Scope
  - 포함
    - 파일 없음, 파싱 실패, 필드 누락 에러 처리
  - 제외
    - `kkachi init`에서의 config 생성은 Phase 2에서 구현
- 필수 테스트
  - Unit
    - 정상·에러 케이스 JSON 파싱
  - Integration
    - temp 디렉토리에 `.kkachi.json` 작성 후 loader 동작 확인

---

### P1-3. Docs hash / pending fix 파일 IO

- 목표  
  docs_hash_file(기본: `.kkachi_docs_hash`), `.kkachi_pending_fix` 파일 읽기·쓰기를 공통 유틸로 제공한다.

- 개발 방향
  - `infra/fs/docs_hash_store.go`
    - `ReadDocsHash(path) (DocsHash, error)`
    - `WriteDocsHash(path, DocsHash) error`
  - `infra/fs/pending_fix_store.go`
    - `PendingFixState` struct (base hash, remote hash, created_at 등)
    - `ReadPendingFix(path) (PendingFixState, bool, error)`
    - `WritePendingFix(path, PendingFixState) error`
    - `RemovePendingFix(path) error`
- 필수 테스트
  - Integration
    - temp 파일 기반 read/write roundtrip

---

### P1-4. HTTP Client 스켈레톤

- 목표  
  kkachi-server endpoint 호출을 위한 공통 HTTP client를 정의한다.

- 개발 방향
  - `infra/http/client.go`
    - `type Client interface {`
      `DocsHead(ctx context.Context, project domain.ProjectName) (...);`
      `RegisterWorkspace(...);`
      `DocsSnapshot(ctx context.Context, project domain.ProjectName, commit string) (...);`
      `DocsPush(ctx context.Context, workspaceID domain.WorkspaceID, base domain.DocsHash, snapshot DocsSnapshot, actorEmail string) (...);`
      `GetState(...);`
      `CreateOrUpdateProject(...);`
      `DeleteProject(ctx context.Context, project domain.ProjectName) (...);`
      `DeleteWorkspace(ctx context.Context, workspaceID domain.WorkspaceID) (...);`
      `}`
    - 내부에서 `http.Client` 사용, base URL + path 조립
    - timeout, status code, JSON decode 에러 처리
- Scope
  - 포함
    - 각 endpoint에 대응하는 메서드 시그니처와 DTO 정의
  - 제외
    - 실제 CLI 기능에서의 호출 로직 (각 Phase에서 사용)
- 필수 테스트
  - Integration
    - `httptest.Server` 기반 fake kkachi-server에 요청 보내기
    - 정상·에러 응답 매핑

---

### P1-5. Conflict marker detector 및 docs diff util

- 목표  
  pre-commit / pre-push / fix에서 재사용할 docs 검사 유틸 구현.

- 개발 방향
  - `domain/merge/conflict_detector.go`
    - 입력: docs 디렉토리 경로
    - 출력: 충돌이 있는 파일 리스트
  - `infra/git/diff.go`
    - `HasDocsChangeStaged(docsDir string) (bool, error)` - commit-msg용
    - `HasDocsChangeForCommit(docsDir string) (bool, error)` - pre-commit용
  - 내부 구현은 `git diff` 호출 또는 go-git 활용 등 선택
- 필수 테스트
  - Unit
    - 텍스트에 conflict 토큰이 있을 때 탐지
  - Integration
    - temp Git repo에서 docs 변경 여부 판단

---

### P1-6. `/docs/push` busy 응답 처리 규약 (선택적)

- 목표  
  서버가 특정 docs repo 에 대한 `/docs/push` 를 이미 처리 중일 때, 두 번째 요청이 **busy 에러**(예: HTTP 409 + `"docs_repo_busy"`)로 돌아오는 경우의 CLI 처리 방식을 정리한다.

- 개발 방향
  - pre-commit, `kkachi fix` 등에서 `/docs/push` 호출 시:
    - 응답이 `"status": "updated" | "nochange" | "outdated"` 인 정상 흐름과,
    - `"error": "docs_repo_busy"` 와 같이 **동시 push 로 인한 busy 에러**인 경우를 구분한다.
  - busy 에러 처리:
    1. 짧은 간격으로 소수의 재시도를 수행한다. v1 기준 기본값은 **최대 3회(초기 호출 + 추가 2회), 재시도 간격 300ms** 로 한다.
    2. 여전히 busy 인 경우
       - pre-commit / fix:
         - "다른 workspace 가 현재 docs 를 업데이트 중입니다. 잠시 후 다시 시도해 주세요." 와 같은 메시지를 출력하고
         - **exit code 1** 로 현재 commit 또는 fix 작업을 차단한다.
       - 필요 시 향후 다른 명령에서는 "best effort" 로 경고만 출력하고 계속 진행하는 정책을 별도로 정의할 수 있다.
  - 이 규약 덕분에, 한 docs repo 에 대한 `/docs/push` 가 동시에 여러 번 들어와도
    - 하나의 요청만 실제 Git 시퀀스를 수행하고,
    - 나머지는 busy 에러와 함께 사용자가 다시 시도하도록 안내된다.

---

## 5. Phase 2 - `kkachi init` 및 `kkachi status`

### P2-1. `kkachi init` domain 및 usecase

- 목표  
  workspace를 kkachi에 등록하고 초기 설정을 생성한다.

- 유즈케이스 흐름

  1. 현재 디렉토리에 `.git` 존재 여부 확인
     - 이미 `.kkachi.json` 이 존재하는 경우, v1 기준 기본 동작은 **init 실패(exit 1)** 로 간주하고 사용자에게 재-init 이 아닌 다른 조치를 안내한다.
       - 예: "이 디렉토리는 이미 kkachi workspace 입니다. 설정을 다시 만들려면 먼저 `.kkachi.json` 을 백업/삭제하거나, 향후 제공될 `kkachi reinit`/`kkachi init --force` 와 같은 명령을 사용해 주세요."
  2. 입력 파라미터 또는 interactive로 project, server URL, docs dir 수집
  3. docs repo URL 입력

     - 사용자가 sudal_docs 등 **문서 전용 repo의 Git URL**(예: `git@github.com:SeventeenthEarth/sudal_docs.git`)을 직접 입력하거나 flag 로 전달한다.
     - 입력받은 docs_repo_url 로부터 **Git URL 의 repo 이름을 그대로 사용해 `docs_repo_id` 를 결정**한다.  
       예: 위 URL이면 `docs_repo_id = "sudal_docs"`.
    - docs_repo_url은 항상 사용자의 명시 입력(또는 별도 관리 도구 결과)에서 가져오며, **코드 repo의 `origin` remote에서 추론하지 않는다**. (Requirement 5.5와 동일 규약)
  4. `POST /projects`로 project가 미등록이면 추가/갱신 (이미 있으면 idempotent no-op)

     - 요청에는 `project`, `docs_repo_id`, `docs_repo_url`, `actor_email` 을 포함한다.
  5. `/workspaces/register` 호출, `workspace_id`, `current_docs_head` 수신
     - 이 때 서버가 `400 Bad Request` 와 `{"error": "unknown_project"}` 와 같은 코드를 반환하면,
       - CLI 는 "kkachi-server에 project가 아직 등록되지 않았습니다. 'kkachi project add'를 먼저 실행해 project 를 등록해 주세요." 와 유사한 안내를 출력하고
       - exit code 1 로 종료한다.
     - 기본 값 규칙:
       - `local_path` 는 현재 작업 디렉토리의 **절대 경로**를 사용한다.
     - `repo_url` 은 현재 코드 repo 의 `origin` URL (`git remote get-url origin`)을 사용한다.
  6. `.kkachi.json` 생성
  7. docs_hash_file(기본: `.kkachi_docs_hash`) 생성 (HEAD hash 기록)
  8. `.kkachi_pending_fix` 존재 시 삭제
  9. Git hook 설치

- 개발 방향
  - `usecase/init_workspace.go`
    - infra에 의존하는 interface:
      - `GitRepoDetector` (`HasGitDir()`)
    - `ServerClient` (`RegisterWorkspace`)
      - `ConfigWriter`
      - `HookInstaller`
- 필수 테스트
  - Unit
    - fake infra로 정상 플로우·에러 플로우 테스트
  - Integration
    - temp Git repo + fake HTTP server 사용

- 추가 요구사항
  - v1 기준 `kkachi init` 완료 시점에는:
    - workspace 루트에 기존 `docs/` 디렉토리가 **없어야** 하며,  
      만약 이미 있다면 init 을 실패(exit 1)로 처리하고 사용자가 먼저 백업/정리하도록 안내한다.
    - 서버 `docs repo` 의 **HEAD 기준 snapshot** 을 내려받아 새 `docs/` 디렉토리를 만든 뒤,  
      이 HEAD 값을 docs_hash_file(기본: `.kkachi_docs_hash`) 에 기록해야 한다.  
      즉, init 이후에는 항상 "서버 HEAD = 로컬 docs" 상태에서 출발한다.
    - 이 snapshot 단계는 서버 로드맵의 `GET /docs/snapshot` 구현(서버 Phase 3)이 준비된 이후에야 실제 kkachi-server 와 end-to-end 로 동작할 수 있으며,
      그 전에는 fake server 또는 stub HTTP client 를 사용해 usecase/CLI 레벨까지 선 구현할 수 있다.

---

### P2-2. Git hook installer infra

- 목표  
  `.git/hooks`에 필요한 hook을 설치하는 infra 구현.

- 개발 방향
  - `infra/git/hook_installer.go`
    - `InstallHook(name string, line string) error`
      - 파일이 없으면 새로 생성
      - 이미 존재하면 `line` 이 포함되어 있는지 검사 후 없으면 append
      - `chmod +x` 수행
  - `kkachi init` usecase에서 다음 hook 설치
    - `pre-commit` → `kkachi hook pre-commit`
    - `post-checkout` → `kkachi hook post-checkout`
    - `post-merge` → `kkachi hook post-merge`
    - `post-rewrite` → `kkachi hook post-rewrite`
    - `pre-push` → `kkachi hook pre-push`
    - `commit-msg` → `kkachi hook commit-msg`
- 필수 테스트
  - Integration
    - temp `.git/hooks` 디렉토리 기반 install test

---

### P2-3. `kkachi init` CLI interface

- 목표  
  실제 CLI 입력을 받아 init usecase를 실행한다. 이때 현재 Git repo 의 `user.email` 을 actor 로 사용한다.

- 개발 방향
  - root command에 `init` subcommand 추가
  - flag 예시
    - `--server-url`
    - `--project`
    - `--docs-dir`
    - 이후 interactive 모드는 선택 옵션
  - 성공 시 summary 출력

    ```text
    kkachi: workspace initialized.
      workspace_id : <id>
      docs_head    : <hash>
    ```

---

### P2-4. `kkachi status` usecase 및 CLI

- 목표  
  현재 workspace docs 기준 버전과 서버 HEAD의 관계를 요약해서 보여준다.

- 유즈케이스 흐름

  1. `.kkachi.json`, `.kkachi_docs_hash`, `.kkachi_pending_fix` 로드
  2. `/docs/head` 호출
  3. H_base와 H_head 비교, DocsStatus 계산
  4. pending_fix 여부 포함해 status view model 생성
  5. `.kkachi_pending_fix`는 존재하지만 docs 디렉토리에 충돌 마커가 하나도 없으면 stale로 간주하고, 이미 수동 해결을 끝낸 것으로 추정하여 `kkachi fix` 실행 또는 파일 삭제를 안내한다.

- 예시 출력

  ```text
  kkachi status
    workspace     : <workspace_id>
    docs base     : <H_base>
    docs head     : <H_head>
    status        : outdated
    pending_fix   : yes

  kkachi: docs base와 서버 HEAD가 다릅니다.
  kkachi: commit 시 pre-commit에서 merge가 발생할 수 있습니다.
  ```

- 필수 테스트
  - Unit
    - 상태 계산 로직
  - Integration
    - fake server로 HEAD 응답 조작 후 다양한 상태 출력 확인

---

### P2-5. Workspace/Project 등록·제거 명령

- 목표  
  kkachi-server에 등록된 project 및 workspace를 CLI에서 **현재 디렉토리와 무관하게** 추가/제거할 수 있게 한다. 로컬 디스크의 디렉토리는 절대 삭제하지 않는다. `kkachi init` 은 기본적으로 `project add` + `workspace register`를 **현재 디렉토리 기준으로 자동 수행**하지만, 별도의 관리용 명령도 제공한다.

- `kkachi project add`
  - 대상: 명시적으로 지정된 project와 docs repo
  - 흐름
    1. project 와 docs repo URL 을 플래그 또는 인터랙티브 프롬프트로 입력받는다.
       - 예: `--project`, `--docs-repo-url` (지정되지 않은 값은 프롬프트로 질문)
       - 입력받은 docs_repo_url 로부터 **Git URL 의 repo 이름을 그대로 사용해 `docs_repo_id` 를 계산**한다.  
         예: `git@github.com:SeventeenthEarth/sudal_docs.git` → `docs_repo_id = "sudal_docs"`.
    2. 현재 Git 설정에서 `user.email` 을 읽어 actor email 로 사용하고, 설정되어 있지 않으면 프롬프트로 email 을 입력받는다.
    3. `POST /projects` 호출 (body 에 `project`, `docs_repo_id`, `docs_repo_url`, `actor_email` 포함)
    4. 성공 시 추가/갱신 결과 안내
  - 주의사항
    - 현재 working directory의 `.kkachi.json` 이나 project 이름에 **암묵적으로 의존하지 않는다.**
    - 다른 디렉토리에서도 안전하게 관리 작업을 수행할 수 있어야 하므로, 인자/프롬프트 기반 입력을 일관되게 사용한다.

- `kkachi workspace register`
  - 대상: 명시적으로 지정된 workspace 디렉토리
  - 흐름
    1. 등록할 디렉토리 경로를 인자로 받는다. 예: `kkachi workspace register /path/to/workspace`
       - 해당 디렉토리 기준으로 `.git` 존재 여부를 확인
    2. project 이름, repo URL, server URL, actor email 을 플래그 또는 프롬프트로 입력받는다.
       - project / repo URL 은 사용자가 명시 입력하거나, 해당 디렉토리의 Git remote 에서 기본값을 추론할 수 있다(Requirement 5.5 규약 참고).
       - actor email 은 기본적으로 해당 repo 의 `git config user.email` 값을 사용하고, 없으면 프롬프트로 입력받는다.
    3. `POST /workspaces/register` 호출
    4. 실행 전 "이 디렉토리를 workspace로 등록합니다. 진행할까요? (y/N)" 확인 프롬프트(기본 N)
  - 주의사항
    - 현재 디렉토리의 `.kkachi.json` 에 **암묵적으로 의존하지 않는다.**
    - 여러 workspace 를 한 곳에서 관리하는 스크립트에서 호출 가능해야 한다.

- `kkachi workspace unregister`
  - 대상: 명시적으로 지정된 workspace_id
  - 흐름
    1. 필수 인자 예시: `--workspace-id` (필요 시 별도 `--project` 보조 인자)
    2. 실행 전 "서버에서 workspace 등록을 제거합니다. 로컬 디렉토리는 삭제되지 않습니다. 진행할까요? (y/N)" 확인 프롬프트 (기본 N)
    3. `DELETE /workspaces/{workspace_id}` 호출  
       - 성공: 204 또는 200 계열, JSON body 유무는 server-roadmap/requirement 규약에 따르되 CLI 에서는 "workspace 등록이 삭제되었습니다" 정도의 메시지 출력  
       - 등록되지 않은 workspace_id 인 경우: Requirement 5.8 규약에 따라 **404 + `{"error": "unknown_workspace"}`** 를 기준으로 처리하며, CLI 는 `"error"` 코드값을 보고 "서버에 등록되지 않은 workspace입니다" 메시지를 조립한다.
    4. 성공 시 안내 메시지 출력, 로컬 `.kkachi.json` 등은 자동 삭제하지 않고 그대로 둔다(필요 시 사용자가 수동 정리)
  - 주의사항
    - 현재 디렉토리의 `.kkachi.json` 을 자동으로 읽어 workspace_id 를 추론하지 않는다.
     - 서버에서 workspace 를 unregister 한 뒤에도 해당 디렉토리의 `.kkachi.json` 은 그대로 남아 있을 수 있다. 이 상태에서 해당 디렉토리에서 `kkachi status` 나 `kkachi fix` 등을 실행하면 서버에서 `{"error": "unknown_workspace"}` 4xx 응답이 발생하고, CLI 는 Requirement 5.8 의 규약에 따라 "서버에 등록되지 않은 workspace입니다. 'kkachi init' 또는 'kkachi workspace register'를 다시 실행해 주세요." 와 같은 안내 메시지와 함께 exit code 1 로 종료해야 한다.

- `kkachi project delete`
  - 대상: 명시적으로 지정된 project 이름
  - 흐름
    1. 필수 인자 예시: `--project`
    2. "서버에서 project를 제거합니다. 등록된 workspace들은 더 이상 동작하지 않습니다. 진행할까요? (y/N)" 프롬프트 (기본 N)
    3. `DELETE /projects/{project}` 호출
       - 성공: 2xx 응답 시 "project가 삭제되었습니다. 이 project에 연결된 workspace들은 더 이상 kkachi 와 통신할 수 없습니다." 정도의 메시지 출력
       - 없는 project: Requirement 5.8 규약에 따라 **404 + `{"error": "unknown_project"}`** 를 기준으로 처리하며, "서버에 해당 project가 존재하지 않습니다." 메시지와 함께 exit code 1
       - 해당 project 에 연결된 workspace 가 남아 있는 경우: Requirement 5.4/5.8 규약에 따라 **409 + `{"error": "project_has_workspaces"}`** 를 기준으로 처리하며,
         "아직 등록된 workspace가 남아 있어 project를 삭제할 수 없습니다. 먼저 관련 workspace를 unregister 하거나, 필요 시 서버에서 force=true 옵션으로 삭제해야 합니다." 와 같은 안내 메시지와 함께 exit code 1
    4. 성공 시 안내 메시지, 남아 있는 로컬 workspace들은 재-init 필요함을 알림
  - 테스트: fake server DELETE 성공/404/409(force 미지정) 케이스 검증
  - 구현 시기
    - C7 은 최소 동작(단순 DELETE 호출 및 기본 프롬프트/메시지)은 Phase 2 에서 구현하고,
      workspace·state UX 개선이나 추가 검증 로직은 필요 시 Phase 3 이후에 확장한다.

---

* C7 의 Phase 2~3 표기는 위와 같은 이유로, Phase 2 에서 기본 기능을 제공하고 이후 Phase 에서 점진 개선 가능함을 의미한다.

---

## 6. Phase 3 - read-only hook (`post-checkout`, `post-merge`, `post-rewrite`)

### P3-1. Hook 공통 interface

- 목표
  hook 서브커맨드 공통 entry를 정의한다.

- 개발 방향
  - `kkachi hook <name> [args...]` 패턴으로 서브커맨드 구성
  - `hook` root 하위에 `pre-commit`, `post-checkout` 등 자식 command 등록

---

### P3-2. `post-checkout` / `post-merge` / `post-rewrite` usecase

- 목표
  각각의 hook에서 적절히 `kkachi status` 를 실행하고, 실패하더라도 Git 동작을 막지 않는다.

- 공통 요구사항
  - 네트워크 오류 또는 설정 없음 등의 경우, 경고 로그만 출력하고 exit code는 0으로 유지한다.
  - 가능한 한 조용하게 동작하고, 상태가 의미 있게 변했을 때만 메시지를 출력하는 옵션도 고려.
- 구현 방향
  - 각 hook usecase는 기본적으로 `ShowStatus` usecase를 재사용
    - `ShowStatus` 가 `.kkachi.json` 부재 등으로 실패하더라도, hook 레벨에서는 이 오류를 swallow 하고 **항상 exit code 0** 을 반환해 Git 동작을 막지 않는다.
  - `post-rewrite` 의 경우:
    - `$1` 인자가 `"rebase"` 일 때만 status 호출
    - 그 외에는 no-op 또는 간단한 로그

---

### P3-3. CLI 연결 및 테스트

- Integration Test
  - temp Git repo 생성
  - `.kkachi.json` 및 hash 파일 작성
  - fake HTTP server로 HEAD 응답 제공
  - `kkachi hook post-checkout` 실행 결과 검증

---

## 7. Phase 4 - `pre-commit` / `commit-msg` 구현

Phase 4는 문서 동기화의 핵심 흐름을 담당한다.

### P4-1. Docs snapshot 생성 domain 및 infra

- 목표
  docs 디렉토리를 tar.gz + base64 로 패키징하는 기능을 구현한다.

- 개발 방향
  - `domain/docs/snapshot.go`
    - `type DocsSnapshot []byte`
  - `infra/fs/snapshot_builder.go`
    - 입력: docs 디렉토리 경로
    - 출력: tar.gz 바이트 배열
      - 이 때 tar 내부의 경로는 `.kkachi.json` 의 `docs_dir` 값과 무관하게 항상 `docs/` 를 루트로 사용하도록 재매핑한다.
        예를 들어 로컬 `docs_dir = "my_docs"` 인 경우에도, tar 안에서는 `docs/..` 경로로만 나타나야 하며,
        서버는 각 project 에 매핑된 docs repo 의 `docs/` 디렉토리에만 내용을 반영한다. (Requirement 6.1.1 과 동일 규약)
  - `infra/fs/snapshot_applier.go`
    - 입력: base64-decoded tar.gz, target docs 디렉토리 경로(`docs_dir`)
    - 동작: tar 내부 `docs/` 루트를 `.kkachi.json` 의 `docs_dir` 경로로 리매핑하여 로컬 디렉토리에 복원한다.
  - `infra/http` client에서 base64 인코딩 후 `/docs/push` 요청 payload에 넣는다.
- 필수 테스트
  - Integration
    - temp docs 디렉토리 → snapshot → 다시 풀었을 때 동일 내용 확인

---

### P4-2. `pre-commit` usecase

- 목표
  commit 직전에 docs 상태를 검사하고, 가능하면 자동으로 연결된 docs repo에 반영하거나, outdated 흐름을 시작한다.

- 유즈케이스 상세 흐름

1. `.kkachi.json`, `.kkachi_docs_hash` 로드

   - 둘 중 하나라도 없거나 파싱에 실패하면 "kkachi 설정(.kkachi.json / .kkachi_docs_hash)이 깨졌습니다." 와 같은 메시지를 출력하고 **exit 1** 로 종료한다.
   - pre-commit 단계에서 설정이 올바르지 않으면 commit 이 진행되지 않도록 하는 것이 목표다.
2. docs conflict 마커 검사

   - 존재하면 메시지 출력 후 exit 1
3. pending fix 상태 검사

   - `.kkachi_pending_fix` 파일이 존재하면, 이 workspace 는 이전 pre-commit 에서 outdated merge 가 발생한 이후 아직 `kkachi fix` 가 완료되지 않은 상태이다.
   - 이 경우 docs 에 conflict 마커가 남아 있다면 2번 단계에서 이미 에러로 처리되었어야 한다.
   - conflict 마커는 없지만 `.kkachi_pending_fix` 가 존재한다면, 다음과 같이 안내하고 **exit 1** 로 commit 을 차단한다.

     ```text
     kkachi: 이 workspace는 이전에 docs outdated가 발생해 pending fix 상태입니다.
     kkachi: docs의 충돌을 모두 해결했다면 'kkachi fix'를 먼저 실행해 연결된 docs repo에 반영해 주세요.
     kkachi: pending fix를 정리하기 전에는 commit을 진행할 수 없습니다.
     ```

4. docs 변경 여부 확인

   - 변경 없음이면
     - "docs change not detected" 정도의 로그만 출력하고
     - exit 0
5. docs snapshot 생성
6. `/docs/push` 호출

   - base_docs_hash = `.kkachi_docs_hash`
   - actor_email = `.kkachi.json` 의 `actor_email` (Requirement 6.2 에서 init 시 저장한 값)
   - `.kkachi.json` 의 `actor_email` 이 비어 있거나 잘못된 경우에는, Requirement 6.2 에서 정의한 fallback 규칙(현재 repo 의 `git config user.email` 재확인 또는 프롬프트 입력)을 적용할 수 있다.

7. 응답 처리

   - `status = "updated"`
     - `.kkachi_docs_hash = new_docs_hash` 로 갱신
     - 성공 메시지 출력 후 exit 0
   - `status = "nochange"`
     - `.kkachi_docs_hash` 를 항상 서버가 알려주는 HEAD(`current_docs_hash`)로 맞춘 뒤
     - exit 0
   - `status = "outdated"`

     1. 서버에서 base, remote snapshot 다운로드
     2. Local = 현재 docs
     3. Base / Local / Remote에 대해 3-way merge 수행

        - 양쪽에서 수정된 곳은 conflict marker 삽입
     4. merge 결과를 docs 디렉토리에 덮어쓰기
     5. `.kkachi_docs_hash = remote_hash` 로 갱신
     6. `.kkachi_pending_fix` 생성
     7. 사용자에게 conflict 해결 후 `kkachi fix`를 안내
     8. exit 1로 commit 차단
   - `ok = false` 또는 기타 오류(네트워크 불안정 포함)
     - 에러 메시지 출력, exit 1로 commit 차단
     - 특히 HTTP 400 + `{"error": "unknown_docs_commit"}` 와 같이 Requirement 6.3.5 / 5.8 에서 정의한 `"unknown_docs_commit"` 에러인 경우,
       연결된 docs repo 히스토리가 재작성되어 자동 복구가 불가능한 상황으로 간주하고,
       "연결된 docs repo 히스토리가 재작성되어 docs 기준 버전을 복구할 수 없습니다. docs repo 와 로컬 docs 를 수동으로 동기화한 뒤 `.kkachi_docs_hash` 를 재설정하거나 `kkachi init` 을 다시 실행해 주세요." 와 같은 안내를 출력한 뒤 exit 1 로 처리한다.

- 구현 포인트
  - 3-way merge는 `git merge-file` 호출 wrapper로 구현하며, kkachi 바이너리는 git 의존성을 전제로 한다.
  - merge는 파일 단위로 수행, IOError가 발생하면 해당 commit을 막고 상세 안내

---

### P4-3. `pre-commit` CLI entry

- 목표
  Git hook에서 전달된 인자와 관계없이 `kkachi hook pre-commit` 이 위 usecase를 실행하도록 한다.

- 주의사항
  - exit code 0이 아닌 경우 commit이 중단된다.
  - 사용자에게 왜 중단되었는지 명확하게 출력한다.

---

### P4-4. `commit-msg` usecase

- 목표
  docs가 변경된 commit의 메시지에 `docs-version: <hash>` 라인을 추가한다.

- 유즈케이스 흐름

1. `.kkachi.json` 로드
2. staged docs 변경 여부 검사

   - 변경이 없으면 그대로 exit 0
3. 메시지 파일에 이미 `docs-version:` 존재하는지 검사

   - 존재하면 변경 없이 exit 0
4. `docs_hash_file`(기본: `.kkachi_docs_hash`) 읽기

   - 값이 없으면 경고 후 exit 0
5. 메시지 파일 끝에 빈 줄 + `docs-version: <hash>` 추가

- 주의사항
  - hook 실행 순서 상 pre-commit 이후에 실행되므로 `.kkachi_docs_hash` 는 최신 docs repo HEAD를 가리켜야 한다.
  - 메시지 파일 인코딩(UTF-8) 유지
- 필수 테스트
  - Integration
    - temp Git repo에서 staged docs 변경 후 commit-msg hook 호출 시 메시지 파일 변경 여부 확인

---

## 8. Phase 5 - `kkachi fix` 및 `pre-push`

### P5-1. `kkachi fix` usecase

- 목표
  outdated merge 이후 사용자 수정이 완료된 docs를 연결된 docs repo에 반영하고 상태를 정상화한다.

- 유즈케이스 흐름

1. `.kkachi.json`, `.kkachi_docs_hash`, `.kkachi_pending_fix` 로드

   - `.kkachi.json` 또는 `.kkachi_docs_hash` 를 읽지 못하면 설정이 깨진 상태이므로 에러 메시지를 출력하고 **exit 1** 로 종료한다.
   - `.kkachi_pending_fix` 파일이 **존재하지 않으면**, pre-commit 에서 outdated merge 를 수행한 흔적이 없는 것이므로

     - "현재 workspace 에는 pending fix 상태(.kkachi_pending_fix)가 없습니다." 와 같은 메시지를 출력하고
     - 아무 작업도 수행하지 않은 채 **exit 1** 로 종료한다.
     - 즉, `kkachi fix` 는 **항상 `.kkachi_pending_fix` 가 존재하는 상황(실제 outdated merge 이후)** 에서만 사용한다.
2. docs conflict 마커 검사

   - 존재하면 에러 메시지 출력 후 exit 1
3. `/docs/head` 호출

   - H_head 수신
4. H_base = `.kkachi_docs_hash` 와 H_head 비교

- 케이스 A: `H_base == H_head`
  - docs snapshot 생성
  - `/docs/push` 호출
    - base_docs_hash = H_base
    - actor_email = `.kkachi.json` 의 `actor_email` (pre-commit 과 동일하게, init 시점의 actor 를 기본으로 사용)
    - `.kkachi.json` 의 `actor_email` 이 비어 있거나 잘못된 경우에는 Requirement 6.2 의 규약에 따라 현재 repo 의 `git config user.email` 을 다시 읽거나, 사용자에게 email 입력을 요구한 뒤 그 값을 사용한다.
  - `status = updated`
    - `.kkachi_docs_hash = new_docs_hash` 로 갱신
    - `.kkachi_pending_fix` 삭제
    - 성공 메시지 출력 후 exit 0
  - `status = nochange`
    - `.kkachi_docs_hash`를 `H_head`로 재기록해 해시를 명시적으로 최신화
    - `.kkachi_pending_fix` 삭제
    - "변경 사항 없음" 로그 출력 후 exit 0
- 케이스 B: `H_base != H_head`
  - 그 사이에 다른 workspace에서 docs repo를 변경한 상황
  - 동작
    - 에러 메시지 출력

      ```text
      kkachi: fix를 시도하는 동안 docs repo HEAD가 변경되었습니다.
      kkachi: 이전 pending fix 상태를 정리하고, 다음 git commit 시 pre-commit에서
              최신 HEAD 기준으로 merge를 다시 수행합니다.
      kkachi: docs 내용과 Git diff를 한 번 확인한 뒤 필요한 경우 다시 commit을 시도해 주세요.
      kkachi: 필요하다면 이 시점의 docs 상태를 'git stash' 또는 별도 브랜치/복사본으로
              백업해 둔 뒤 새로운 merge 결과를 적용하는 것을 권장합니다.
      ```

    - `.kkachi_pending_fix` 파일을 **삭제**해, workspace 를 pending fix 상태에서 해제한다.
      - `.kkachi_docs_hash` 는 기존 H_base 값을 유지한다.
      - 이후 pre-commit 은 `.kkachi_pending_fix` 가 없으므로 `/docs/push` 를 다시 호출할 수 있고,
        outdated 가 감지되면 최신 HEAD 기준으로 새로운 3-way merge 를 수행한다.
    - exit 1 로 종료 (현재 fix 시도는 실패로 보고, 사용자가 이후 commit 을 통해 새로운 merge 를 수행하도록 유도)
    - 이 때 출력 메시지와 exit code 규약은 requirements 6.4.2(kkachi fix 동작 요구사항)와 동기화하여 관리한다.
  - HTTP 400 + `{"error": "unknown_docs_commit"}` 와 같이 Requirement 5.8 / 6.4.2 에서 정의한 `"unknown_docs_commit"` 에러가 발생한 경우,
    pre-commit 에서의 처리와 동일하게 "연결된 docs repo 히스토리가 재작성되어 현재 pending fix 상태에서는 자동 복구가 불가능합니다. docs repo 와 로컬 docs 를 수동으로 동기화한 뒤 `.kkachi_docs_hash` 를 재설정하거나, 필요 시 `kkachi init` 을 다시 실행해 주세요." 정도의 안내를 출력하고 exit 1 로 종료한다.
- 필수 테스트
  - Unit
    - 두 케이스 흐름 검증
  - E2E
    - 실제 서버·repo 기반 outdated 시나리오 재현

---

### P5-2. `pre-push` usecase

- 목표
  push 직전 docs 상태를 검증해 문제 상황을 원격으로 전파하지 않도록 한다.

- 유즈케이스 흐름

1. `.kkachi.json` 로드

   - 파일이 없거나 파싱에 실패하면 pre-push 단계에서 설정이 깨진 것이므로 에러 메시지를 출력하고 **exit 1** 로 종료한다.
2. docs conflict 마커 검사

   - 존재 시 경고 메시지 후 exit 1
3. `.kkachi_pending_fix` 검사

   - 존재 시 경고 메시지 후 exit 1
4. 선택적으로 `kkachi status` 를 간단히 출력
5. 위 조건에 걸리지 않으면 exit 0

- 구현 포인트
  - pre-push에서는 서버와 통신하거나 자동 fix를 시도하지 않는다.
  - 단순 검증 및 차단 역할에 집중한다.

---

### P5-3. `kkachi hook pre-push` CLI entry

- 목표
  위 usecase를 hook entry로 연결하고, Git pre-push 프로토콜 인자(`remote name`, `remote url`)는 현재 구현에서는 사용하지 않는다.

- Integration 테스트
  - temp Git repo에서 hook script를 실제로 실행
  - `.kkachi_pending_fix` 존재 여부에 따른 exit code 확인

---

### P5-4. `kkachi state` 및 `kkachi state --all`

- 목표  
  kkachi-server가 가진 전체 workspace 상태 정보를 확인하되, 기본 사용에서는 현재 project에 해당하는 정보만 추려서 보여준다.

- 유즈케이스 흐름 (`kkachi state`)

  1. 현재 디렉토리의 `.kkachi.json` 로드, `project` 값을 획득
  2. `/state` 호출
  3. 응답의 workspace 리스트 중 `project` 가 현재 workspace 의 project 와 일치하는 항목만 필터링
  4. `/state` 응답의 `docs_heads[project]` 값을 현재 project 의 `docs_head` 로 사용하고,  
     해당 project에 속한 workspace 들의 docs_hash, last_reported_at 등을 함께 요약해서 출력

- `kkachi state --all`

  - UX 요구사항:
    - `kkachi state --all` 을 실행하면 **모든 project의 workspace 정보**를 한 번에 볼 수 있어야 한다.
  - 구현 방향:
    - `--all` 플래그가 지정된 경우 `/state` 응답을 필터링 없이 그대로 출력하는 모드를 제공한다.
    - 표 형식 예시:

      ```text
      kkachi state --all
        docs_heads:
          sudal    : <H_sudal>
          dolgora  : <H_dolgora>
        workspaces:
          - project: sudal    workspace_id: sudal:/Users/...       docs_hash: ... last_reported_at: ...
          - project: sudal    workspace_id: sudal:/Users/..._app   docs_hash: ... last_reported_at: ...
          - project: dolgora  workspace_id: dolgora:/Users/...     docs_hash: ... last_reported_at: ...
      ```

- 주의사항
  - `/state` 는 내부 운영/디버그용 endpoint 이므로, CLI 출력 포맷은 사람이 읽기 편한 수준이면 충분하다.
  - `.kkachi.json` 이 없을 때 `kkachi state` 를 호출하면 "현재 디렉토리가 kkachi workspace 가 아님" 정도의 친절한 메시지를 출력하고 종료한다.

---

## 9. 향후 확장 아이디어 (CLI 관점)

요구사항은 아니지만, 추후 고려할 수 있는 확장 아이디어는 다음과 같다.

- `kkachi pull`
  - 연결된 docs repo의 최신 docs snapshot을 받아 로컬 docs를 덮어쓰는 명령
  - 현재는 `fix` 및 pre-commit 중심 설계이지만, 특정 시점에 “강제 최신화”가 필요할 수 있다.
- `kkachi status --json`
  - 다른 도구에서 파싱하기 쉬운 JSON 포맷 상태 출력
  - 예시 스키마 (초안):

    ```json
    {
      "workspace_id": "sudal:/Users/...",
      "project": "sudal",
      "docs_base": "a1b2c3d4",
      "docs_head": "f5e6g7h8",
      "status": "up_to_date | outdated | unknown",
      "pending_fix": true,
      "pending_fix_stale": false
    }
    ```

    - 여기서 `status` 는 CLI Status (HEAD 비교 결과)를 의미하고, `pending_fix` / `pending_fix_stale` 는 `.kkachi_pending_fix` 상태를 표현한다. 다른 도구나 Web UI 는 이 세 필드를 함께 사용해 "현재 workspace 가 commit/push 가능한 상태인지" 를 판단할 수 있다.
- Interactivity 개선
  - outdated 발생 시 간단한 요약(diff)와 함께 어떤 파일에서 충돌이 났는지 정리 출력
- 로컬 캐시
  - 빈번한 `/docs/head` 호출 최적화를 위한 short-lived 캐시
- 멀티 workspace 지원 UX
  - 한 CLI 인스턴스에서 여러 workspace의 상태를 한눈에 보는 `kkachi workspaces` 명령 추가
