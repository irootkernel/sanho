# Sanho 운영 가이드

설치, 상주 실행, 업그레이드, 제거의 소유권과 절차는
[배포 규칙](deployment.md)을 따른다.

## 빌드와 실행

```bash
make daemon-build
make cli-build
make daemon-run
```

로컬 checkout에서 두 실행 파일을 Go 설치 경로에 넣으려면 `make install`을
사용한다. 공개 release는 다음처럼 저장소에서 직접 설치한다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.3
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.3
sanho version
sanhod --version
```

개발 중에는 `make daemon-run-dev`로 `go run`을 사용할 수 있다. daemon은
별도의 frontend build나 hot-reload 도구를 요구하지 않는다.

런타임 경로를 바꾸는 환경 변수는 두 개다.

| 변수 | 기본값 | 설명 |
|---|---|---|
| `SANHO_HOME` | `~/.sanho` | state와 docs clone을 보관하는 daemon 전용 디렉터리 |
| `SANHO_SOCKET` | `$SANHO_HOME/sanhod.sock` | Unix socket의 절대 경로 |

state는 `$SANHO_HOME/state.json`, docs clone은 프로젝트 등록 시
`$SANHO_HOME/docs_repos/<docs_repo_id>` 아래에 생성된다. daemon은 home과
docs clone 디렉터리를 `0700`, state와 socket을 `0600`으로 관리한다.
`.sanho*`와 인증 정보는 commit하지 않는다.

## 서비스 관리

daemon은 foreground 프로세스로만 제공한다. 로그인 또는 부팅 시 자동 실행,
재시작, 로그 보존은 사용자가 launchd나 systemd 같은 운영체제 service
manager에 직접 등록해 관리한다. 저장소의 Make target은 service를 설치하거나
시작하지 않는다.

## 상태 확인

```bash
curl --fail --unix-socket ~/.sanho/sanhod.sock http://sanho/healthz
sanho state --all
sanho state --all --json
sanho --socket /absolute/path/to/sanhod.sock state --all
```

정상 health 응답은 `{"ok":true}`다. `/state`는 등록된 모든 프로젝트의
원격 HEAD를 갱신하므로 Git fetch나 SSH 오류가 있으면 전체 요청이
실패한다. 일부 프로젝트만 stale 값으로 표시하지 않는 의도적인 정책이다.

## 장애 대응

### 진행 중이거나 stale인 Git operation

Sanho는 refs와 worktree의 겉보기 상태만으로 변경 가능 여부를 판단하지 않는다.
`HEAD == origin/main`이고 `git status --porcelain`이 비어 있어도 Git의
rebase, merge, cherry-pick, revert, bisect, `git am` 또는 sequencer metadata가
남아 있으면 작업공간 변경을 차단한다. `.git`이 디렉터리인지 파일인지
추측하지 않고 `git rev-parse --git-path`로 현재 worktree의 metadata 경로를
해석하므로 linked worktree의 상태도 서로 섞이지 않는다.

다음 명령은 operation을 먼저 정리할 때까지 실패한다.

- `init`, `pull`, `fix`, 실제 `clean`
- `pull-commit`과 `--continue`, `--abort`, `--recover`
- pre-push와 main 선행 게시

`sanho status`와 `sanho status --json`, `clean --dry-run`은 현재 application
workspace에 대해 읽기 전용으로 계속 사용할 수 있다. daemon은 project
status를 계산하기 위해 자신이 관리하는 canonical docs clone을 refresh할 수
있지만 application workspace의 refs, index, worktree나 operation metadata는
변경하지 않는다. 상태 출력의 `git_operation`에서 감지된 종류, 차단 이유와
후보 명령을 확인한다. 항상 다음 명령으로 실제 Git 상태를 먼저 읽는다.

```bash
git status
```

rebase라면 의도에 따라 다음 중 하나를 선택한다.

```bash
git rebase --continue
git rebase --abort
git rebase --quit
```

`--abort`는 rebase 시작 전 branch 위치와 상태를 복원한다. `--quit`은 현재
HEAD, index와 worktree를 유지한 채 rebase metadata만 종료한다. 따라서
`--quit`을 일괄적인 stale-state 정리 명령으로 사용하지 않는다. merge,
cherry-pick, revert와 `git am`도 `git status`를 확인한 뒤 해당 명령의
`--continue`, `--abort`, 필요 시 `--quit`을 선택한다. bisect는
`git bisect log`로 기록을 확인하고 계속 판정하거나 `git bisect reset`으로
종료한다.

Sanho는 어떤 경우에도 사용자 operation을 자동 abort/quit하거나 metadata를
삭제하지 않는다. `.git/rebase-*`, `.git/sequencer` 같은 경로를 손으로
삭제해서도 안 된다. detector가 여러 operation marker를 발견하면
`multiple`, 종류를 안전하게 판별할 수 없는 sequencer는 `sequencer`로
보고하고 `git status` 외의 정리 명령을 추측하지 않는다.

Git의 continue 과정에서 실행되는 `pre-commit`, `commit-msg`, `post-commit`,
`post-checkout`, `post-merge` hook은 Sanho의 index/worktree/ref 변경과 daemon
게시를 건너뛰고 경고 후 성공한다. `post-rewrite rebase`는 Git이 성공한 rebase
뒤 제공한 old/new commit mapping이 모두 유효하고 모든 새 commit이 현재
HEAD에서 도달 가능할 때만 예외다. 이때 pull-commit mapping, docs hash와
workspace 보고만 reconciliation하며 Git refs, index, worktree와 operation
metadata는 변경하지 않는다. mapping이 비었거나 검증에 실패하면 다른 hook과
같이 무변경으로 종료한다. pre-push는 복구에 필요하지 않고 원격 변경을
일으키므로 active operation 동안 계속 실패한다.

### `docs_repo_busy`

같은 docs repo에서 다른 sync, read, push, delete가 진행 중이다. 진행 중인
작업이 끝난 뒤 다시 시도한다. 잠금을 우회해 clone을 직접 수정하지 않는다.

### `outdated`

다른 작업공간이 먼저 docs origin을 갱신했다. Git commit 중이라면
pre-commit hook이 `pull-commit` 흐름을 자동 실행한다. 충돌이 없으면 원격
docs만 담은 `[SANHO] Update docs` 커밋을 최신 `main` 위에 만들고 현재
staged/unstaged 변경을 보존한 뒤 commit을 한 번 중단한다. 같은
`git commit` 명령을 다시 실행하면 원래 변경을 이어서 커밋한다.

동기화 전에 `origin/main`을 fetch한다. 로컬 `main`이 뒤처졌다면 최신
원격 commit을 기준으로 하고, 로컬이 앞서 있다면 로컬 `main`을 기준으로
한다. 둘이 갈라졌다면 자동으로 어느 쪽도 선택하지 않고 실패한다.
현재 branch가 `main`이면 그대로 유지한다. 다른 branch라면 원격에
게시되지 않았고 merge commit이 없는 경우에만 새 docs system commit 위로
자동 rebase한다. upstream이나 같은 이름의 원격 branch가 있는 published
branch는 history를 바꾸지 않고 실패한다. 이 과정은 임시 clone에서 먼저
검증하고, 성공했을 때만 로컬 `main`과 현재 branch ref를 함께 갱신한다.
system commit은 `origin/main` 도달 여부를 확인할 때까지 게시 대기 상태로
기록한다.

`git push origin main`은 로컬 `main` 전체를 원래 push로 게시한다. 다른
origin branch를 push하면 pre-push가 먼저 로컬 `main` 전체를
`origin/main`에 fast-forward push한다. main 게시를 확인한 뒤에만 원래
target push를 계속한다. 로컬 main에 일반 commit이 섞여 있어도 main에 둔
게시 의도를 우선해 함께 게시한다. 원격 main이 먼저 변경됐거나 권한,
branch protection, network 문제로 main push가 실패하면 target push도
차단한다. force push나 별도 `sanho push` 명령은 사용하지 않고 같은
`git push`를 재시도한다.

게시 대기 상태에서 main과 다른 branch를 한 push로 함께 갱신하면 remote의
부분 성공 여부를 통제할 수 없으므로 차단한다. 이 경우 `origin/main`을 먼저
push한 뒤 나머지 ref push를 다시 실행한다.

main 선행 게시만 성공하고 target push가 실패한 경우 게시 대기 상태는 이미
정리된다. 다음 재시도는 target만 push한다. direct main push의 성공 여부는
hook이 사후 관찰할 수 없으므로 다음 `sanho status` 또는 origin branch
push가 `origin/main`을 fetch해 상태를 멱등하게 정리한다.

게시 대기 상태에서 `origin`이 아닌 remote 이름, 같은 URL을 가리키는 alias,
또는 remote URL을 직접 지정해 branch를 push하면 Sanho는 이를 자동 게시
대상으로 해석하지 않고 차단한다. 먼저 `git push origin main`을 실행한 뒤
원래 push를 재시도한다. 게시 대기 상태가 해소되면 다른 remote와 직접 URL의
branch push도 허용된다. tag-only push와 ref 삭제는 이 규칙의 영향을 받지
않는다.

텍스트 충돌이 있으면 파일을 해결하고 stage한 뒤
`sanho pull-commit --continue`를 실행한다. 시스템 커밋 생성 전에는
`sanho pull-commit --abort`로 원래 상태를 복원할 수 있다. 원격
history를 강제 push로 덮어쓰지 않는다.

`git commit --amend`는 준비된 commit을 sibling commit으로 바꾸므로 단순한
ancestor 검사만으로 완료 여부를 판단하지 않는다. `post-rewrite`가 stdin의
old/new 매핑과 준비 당시 index tree를 검증한다. hook이 중단된 경우
`sanho pull-commit --recover`가 commit graph, parent/tree와 rewrite 기록을
검사한다. 복구 전에는 현재 HEAD, index, worktree를
`refs/sanho/recovery/<transaction-id>/`에 보존하며, 완료를 증명할 수 없으면
transaction을 삭제하지 않는다.

버전 1·2의 legacy transaction은 준비 당시 tree와 rewrite mapping이 없다.
따라서 같은 parent를 가진 sibling commit이어도 저장된 `merged-index.tar.gz`와
현재 HEAD의 docs tree가 내용과 Git file mode, symbolic link 구조까지
일치할 때만 `recoverable_rewrite`로 처리한다. snapshot 누락·손상은
`corrupt`, docs 불일치는 `ambiguous`다. 이때 `.git/sanho`를 직접 삭제하거나
backup ref를 제거하지 말고 `sanho status`의 `reason`과 `next_command`를
기록한 뒤 보존된 `refs/sanho/recovery/<transaction-id>/`에서 필요한 Git
상태를 확인한다. 원래 의도를 확인할 수 없다면 자동 정리하거나 push하지
않는다.

pre-push는 transaction 디렉터리의 존재만으로 판단하지 않는다. 논리적으로
완료된 stale state는 멱등하게 정리하지만 `pending`, `ambiguous`, `corrupt`
상태는 계속 차단하고 `status`와 같은 안전한 다음 명령을 출력한다.
기존 workspace의 pre-push hook이 remote 인자를 전달하지 않으면 게시 대기
상태를 임의 remote에 적용하지 않는다. Sanho가 hook을 제자리에서 갱신하고
현재 push를 중단하므로 같은 push를 한 번 다시 실행한다.

실제 remote, branch rule, 인증과 service manager를 포함한 릴리스 검증은
[hands-on 테스트](hands-on-testing.md)의 공통 증거 양식과 시나리오를 따른다.

바이너리는 base/local/remote 내용 비교만으로 결과가 명확한 경우 자동으로
처리한다. local과 remote가 base와 서로 다르게 변경된 바이너리는 경로를
포함한 오류로 중단하고 worktree, index, docs hash와 transaction 상태를
변경하지 않는다. 이 경우 `sanho fix`나 `pull-commit --continue`가 아니라
중앙 문서 또는 로컬 파일을 원하는 내용으로 일치시킨 뒤 `pull-commit`을
다시 실행한다.

`pull`, system commit, 일반 사용자 commit은 성공한 docs hash를 daemon에
보고한다. 일반 commit은 `post-commit` hook이 보고하며, hook을 실행하지
않는 system commit과 commit을 만들지 않는 `pull`은 CLI가 명시적으로
보고한다. 보고가 실패하면 pending 상태를 Git metadata 아래에 남긴다.
다음 pull/commit/push가 이를 먼저 재시도하며, daemon에 반영될 때까지 새
commit과 push 및 `clean`을 허용하지 않는다. system commit 보고 실패는
진행 중인 `pull-commit` 트랜잭션 자체가 같은 역할을 한다.

`pull`은 worktree와 docs hash를 즉시 갱신하지만 애플리케이션 commit은
만들지 않는다. 대신 pull 직전 index와 반영한 snapshot을 Git private
metadata에 기록한다. 다음 일반 commit의 pre-commit 또는 명시적
`pull-commit`이 이 기준선을 소비해 `[SANHO] Update docs` commit을 만든다.
pull로 새로 생긴 원격 파일은 사용자가 실제로 stage해 삭제하지 않는 한
로컬 staged 삭제로 해석하지 않는다. `--force` pull은 기존 staged docs도
버리므로 기준선의 원래 index 역시 현재 HEAD 기준으로 다시 잡는다.

실제 `git pull`, rebase/amend, branch checkout처럼 HEAD가 바뀌는 작업은
각각 `post-merge`, `post-rewrite`, `post-checkout` hook에서 HEAD의 docs
tree를 다시 확인한다. 현재 tree와 일치하는 reachable `docs-version`
commit을 찾았을 때만 `.sanho_docs_hash`와 daemon workspace hash를 함께
갱신한다. 일치하는 commit이 없거나 pending pull 위에서 dirty docs가
발견되면 추측해서 보고하지 않고 경고 또는 보호 실패로 남긴다.

### Git fetch 또는 push 실패

SSH key, 저장소 URL, 네트워크, branch 권한을 확인한다.

```bash
git ls-remote <docs-repo-url> HEAD
```

daemon은 실패 후 clone을 origin으로 reset한다. 장애 조사 중에도
`~/.sanho/docs_repos`의 clone에서 임의 commit이나 force push를 만들지 않는다.

### state 손상

primary state 파일이 깨졌다면 daemon은 `<state-path>.bak`에서 자동
복구한다. 둘 다 깨졌을 때는 시작이 실패한다. 이 경우 두 파일을 먼저
별도 위치에 보존하고 JSON과 로그를 조사한다. 빈 state 파일로 덮어써서
문제를 숨기지 않는다.

## 검증

```bash
make test-prepare
make test-unit
make test-int
make test-e2e
```

`make test`는 위 네 단계를 표시된 순서대로 모두 실행한다.
`test-prepare`는 코드 생성, format, 모듈 검증, 문서 검사, daemon/client
패키지 소유권 검사, 아키텍처 guardrail, vet, lint를 수행한다. 각 단계는
`test-prepare-daemon`, `test-unit-client`처럼 `-daemon` 또는 `-client`
접미사가 붙은 target으로 범위를 좁힐 수 있다.

daemon과 client E2E는 기본적으로 임시 home과 Unix socket을 사용하는
독립 daemon을 띄운다. 실행 중인 별도 daemon을 대상으로 확인하려는
경우에만 `E2E_SOCKET`에 절대 socket 경로를 명시한다.

```bash
make test-e2e-daemon
make test-e2e-daemon E2E_SOCKET=/absolute/path/to/sanhod.sock
make test-e2e-client
make test-e2e-client E2E_SOCKET=/absolute/path/to/sanhod.sock
```

통합·E2E 테스트의 docs 저장소에는 실제 운영 저장소 대신 `/tmp` 아래의
폐기 가능한 bare Git 저장소와 clone을 사용한다. 전체 검증은
`make test`로 실행한다.
