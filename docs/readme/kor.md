# Sanho

Sanho는 여러 애플리케이션 저장소의 `docs/` 디렉터리를 전용 docs
저장소와 동기화하고, 여러 작업공간이 동시에 문서를 갱신할 때 생기는
충돌을 막는 도구다.

제품은 두 실행 파일만 사용한다.

- `sanhod`: docs 저장소의 Git 접근과 작업공간 상태를 조정하는 HTTP daemon
- `sanho`: 개발자와 Git hook이 사용하는 CLI

Web UI, 브라우저 터미널, PTY, 세션 실행 기능은 제공하지 않는다.

## 빠른 시작

지원 운영체제는 macOS와 Linux다. 필수 환경은 Go 1.25 이상과 Git이며,
docs 저장소를 읽고 쓸 수 있는 SSH 인증도 준비해야 한다. Node.js와 npm은
필요하지 않다.

release 바이너리는 Go module에서 직접 설치할 수 있다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.5
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.5
```

설치 위치는 `GOBIN`, 설정하지 않았다면 `$(go env GOPATH)/bin`이다.
이 디렉터리를 `PATH`에 포함해야 한다. 설치 확인은 `sanho version`과
`sanhod --version`으로 한다.

```bash
make daemon-build
make cli-build
make daemon-run
```

daemon은 기본적으로 `~/.sanho/sanhod.sock` Unix socket에서 요청을 받고,
state와 docs clone을 각각 `~/.sanho/state.json`,
`~/.sanho/docs_repos/`에 저장한다.

애플리케이션 Git 저장소에서 다음 명령으로 작업공간을 초기화한다.

```bash
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

`init`은 현재 docs snapshot을 내려받고 `.sanho.json`,
`.sanho_docs_hash`를 만든 뒤 문서 동기화용 Git hook을 설치한다.
기본값이 아닌 daemon을 사용하려면 전역 옵션
`--socket /absolute/path/to/sanhod.sock`을 지정한다.

## 일상 작업

```bash
sanho status  # 로컬 기준과 HEAD, 같은 프로젝트 작업공간 비교
sanho status --json  # 자동화 도구용 구조화 상태
sanho pull    # 변경이 없을 때 최신 docs snapshot 반영
sanho pull-commit  # staged/unstaged를 보존하며 최신 docs base commit 생성
sanho fix     # 기존 pending-fix 상태의 merge 결과 게시
sanho state   # 등록된 프로젝트와 작업공간 상태 조회
sanho state --json   # 현재 프로젝트 상태를 JSON으로 조회
```

`sanho status`는 현재 작업공간의 전체 docs commit과 HEAD를 보여주고,
같은 프로젝트에 등록된 모든 작업공간을 현재 로컬
`.sanho_docs_hash` 기준으로 비교한다. 관계는 다음과 같다.

- `same`: 같은 docs commit
- `ahead N`: 대상 작업공간에만 `N`개 commit이 있음
- `behind N`: 현재 기준에만 `N`개 commit이 있음
- `diverged +N/-M`: 양쪽에 서로 다른 commit이 있음
- `unknown`: 저장된 commit을 현재 docs 이력에서 확인할 수 없음

목록은 `ahead`, `same`, `behind`, `diverged`, `unknown` 순으로 표시한다.
이 비교는 각 애플리케이션 저장소의 branch 상태가 아니라 docs 저장소의
commit graph를 기준으로 한다. 새 상태 endpoint가 없는 구버전 daemon과
함께 실행하면 기존 HEAD 비교는 유지하고 작업공간 목록에는 daemon
업그레이드가 필요하다고 표시한다.

`version`, `status`, `state`의 JSON 필드와 오류 계약은
[CLI JSON 출력](../cli-json.md)에 정리되어 있다.
설치, launchd/systemd 등록, 업그레이드와 제거 규칙은
[배포 규칙](../deployment.md)에 정리되어 있다.
실제 remote와 service를 사용하는 릴리스 전 검증은
[hands-on 테스트](../hands-on-testing.md)를 따른다.

로컬 docs 변경이 있을 때 `sanho pull`로 덮어쓰려면 명시적으로
`--force`를 사용해야 한다. 설정만 제거하려면 `sanho clean`을 사용하고,
docs 디렉터리까지 지우려면 `--remove-docs`를 별도로 지정한다.

작업공간에 rebase, merge, cherry-pick, revert, bisect, `git am` 또는 다른
sequencer operation이 남아 있으면 Sanho 변경 명령은 실패한다. HEAD와
`origin/main`이 같고 worktree가 깨끗해도 operation metadata가 있으면 안전한
상태로 간주하지 않는다. 먼저 다음 명령으로 상태를 확인한다.

```bash
git status
sanho status
sanho status --json
```

Sanho가 출력하는 continue/abort/quit 후보 중 사용자 의도에 맞는 Git 명령을
선택한다. rebase의 `--abort`는 시작 전 상태를 복원하고 `--quit`은 현재
HEAD, index와 worktree를 유지한다. Sanho는 operation을 자동 종료하거나
`.git/rebase-*`, sequencer metadata를 삭제하지 않는다. Git 복구 중 lifecycle
hook은 Sanho 변경을 건너뛰지만 pre-push는 원격 게시를 계속 차단한다. 단,
성공한 rebase가 active backend의 Git 소유 rewrite 파일을 stdin으로 넘기고 그
안의 각 line에서 첫 두 full old/new commit OID를 검증할 수 있으면 Git
refs·index·worktree는 건드리지 않고 pull-commit 기록, docs hash와 workspace
보고만 새 HEAD에 맞춘다. pipe나 다른 파일에서 주입한 mapping, 비어 있거나
유효하지 않은 mapping에는 이 예외를 적용하지 않는다.
뒤에 optional extra-info가 있으면 호환성을 위해 opaque 값으로 무시하지만
source 신뢰나 commit 검증에는 사용하지 않는다.

대규모 rebase의 mapping도 commit별 Git process가 아니라 object와 도달 가능성
각 한 번씩의 batch 검사로 검증한다. 검증 결과는 worktree, 검증 당시 HEAD,
rewrite command와 순서가 보존된 전체 mapping에 결속한 비공개 permit으로
reconciliation에 전달하므로 commit tree를 mapping별로 다시 조회하지 않는다.
reconciliation에는 별도의 30초 제한을 적용한다. 실패 시
Git rebase는 완료되더라도 Sanho metadata는 보존되므로 `sanho status --json`과
`sanho pull-commit --recover`로 상태를 확인한다.

Git commit 시 중앙 docs가 갱신된 상태라면 pre-commit hook이
`pull-commit` 흐름을 자동으로 실행한다. 첫 시도에서는 `origin/main`을
fetch하고, 원격 docs만 담은 `[SANHO] Update docs` 커밋을 최신 `main` 위에
만든다. 현재 branch가 `main`이면 branch를 유지하고, unpublished linear
feature branch라면 system commit 위로 local commit을 rebase한다. published
branch, merge commit이 있는 branch, 갈라진 local/remote `main`은 자동으로
history를 바꾸지 않고 실패한다. 성공 시 현재 staged/unstaged 변경을
보존한 뒤 원래 commit을 중단한다. 같은 `git commit` 명령을 다시 실행하면
보존한 staged 변경을 Git의 commit index에 복원해 원래 커밋을 계속한다.
원래 명령이 `git commit --amend`라면 `post-rewrite` hook이 Git의 old/new
commit 매핑을 읽어 준비된 commit의 rewrite를 확인하고 트랜잭션을 종료한다.
연속 amend와 같은 hook 재실행도 같은 결과를 내도록 멱등 처리한다.

생성한 system commit은 애플리케이션 저장소의 `origin/main`에서 확인될
때까지 Git private metadata에 게시 대기 상태로 남는다. `git push origin
main`은 로컬 `main`의 system commit과 사용자 commit을 원래 push 한 번으로
게시한다. 다른 origin branch를 push하면 pre-push가 먼저 로컬 `main` 전체를
`origin/main`에 fast-forward push한 뒤 요청한 branch push를 계속한다. main
게시가 거부되거나 원격과 갈라졌다면 target push도 중단하며 force push하지
않는다. 원인을 해결한 뒤 같은 `git push`를 다시 실행하면 된다.

기존 workspace의 hook이 Git remote 인자를 전달하지 않으면 첫 게시 시도에서
Sanho가 hook을 제자리에서 갱신하고 같은 push를 한 번 다시 요청한다. 별도
`sanho push`나 재초기화 명령은 없다. 게시 대기 중 `origin`이 아닌 remote,
alias 또는 직접 URL로 branch를 push하면 우회 게시를 막기 위해 중단한다.
먼저 `git push origin main`을 완료한 뒤 원래 push를 다시 실행한다. 게시
대기가 없으면 다른 remote push를 허용하며 tag-only와 ref 삭제는 영향을
받지 않는다.

같은 텍스트 파일을 함께 수정했다면 원격을 덮어쓰지 않고 충돌 상태로
멈춘다. 파일을 해결하고 stage한 뒤 `sanho pull-commit --continue`를
실행한다. 시스템 커밋이 만들어지기 전이라면 `sanho pull-commit --abort`로
원래 staged/unstaged docs 상태를 복원할 수 있다.

commit 또는 rewrite hook이 중단되어 transaction이 남았다면
`sanho status`에서 `pull_commit` 분류와 안전한 다음 명령을 확인한다.
완료된 rewrite가 ancestry만으로 확인되지 않으면
`sanho pull-commit --recover`를 실행한다. 이 명령은 먼저
`refs/sanho/recovery/<transaction-id>/` 아래에 현재 HEAD, index, worktree를
보존한 뒤 완료가 증명된 transaction만 정리한다. 판단이 불명확하거나 Git
object가 손상된 경우 state와 backup ref를 유지하고 push를 계속 차단한다.

버전 1·2에서 남은 transaction은 amend 전후 commit의 parent가 같다는
이유만으로 정리하지 않는다. 저장된 merged index snapshot과 현재 HEAD의
docs 내용·실행 비트·symbolic link 구조가 일치해야 한다. snapshot이 없거나
깨졌으면 `corrupt`, docs가 달라졌으면 `ambiguous`로 남는다. 두 경우 모두
`.git/sanho`를 수동 삭제하지 말고 `sanho status`가 안내하는 복구 명령과
`refs/sanho/recovery/<transaction-id>/`의 backup ref를 사용한다.

PNG나 PDF 같은 바이너리 파일도 base/local/remote 중 두 내용이 같아 결과가
명확하면 자동으로 유지하거나 변경된 쪽을 채택한다. local과 remote가 base와
서로 다르게 변경된 바이너리는 자동으로 선택하지 않고 파일 경로를 포함한
오류로 중단한다. 이 오류는 `pull-commit` 트랜잭션을 만들기 전에 발생하므로
`sanho fix`나 `sanho pull-commit --continue`를 사용하지 않는다. 중앙 문서와
로컬 파일 중 유지할 내용을 결정해 두 파일을 일치시킨 뒤 `sanho pull-commit`을
다시 실행한다.

시스템 커밋 제목은 `.sanho.json`의 `docs_sync_commit_message`로 바꿀 수
있으며 기본값은 `[SANHO] Update docs`다.

`pull`과 pull-commit system commit은 CLI가 daemon에 docs hash를 직접
보고하고, 일반 commit은 post-commit hook이 보고한다. 보고 실패는 pending
상태로 저장되며 다음 pull/commit/push에서 먼저 재시도된다. 반영 전에는
새 commit, push, `clean`이 차단되므로 `/state`가 뒤처진 상태로 작업이
계속 진행되지 않는다.

`pull` 자체는 commit을 만들지 않는다. 대신 pull 직전 index와 반영한 docs
snapshot을 Git private metadata에 보관한다. 다음 일반 commit의 pre-commit
또는 명시적 `pull-commit`이 이 기준선을 사용해 system commit을 만든다.
이때 pull로 추가된 원격 파일은 사용자가 실제로 stage해 삭제하지 않은 한
staged 삭제로 바뀌지 않는다. `pull --force`는 staged docs까지 버리는
명령이므로 기준선의 원래 index도 HEAD 기준으로 초기화한다.

`git pull`, rebase/amend, branch checkout으로 HEAD가 이동하면
`post-merge`, `post-rewrite`, `post-checkout` hook이 현재 docs tree와
일치하는 reachable `docs-version` commit을 찾는다. 일치할 때만 로컬 hash와
daemon workspace hash를 함께 갱신한다. 일치하는 commit이 없거나 pending
pull 위에 dirty docs가 남아 있으면 임의의 hash를 선택하지 않는다.

## AI agent 설정

Sanho로 관리하는 프로젝트의 `AGENTS.md` 또는 `CLAUDE.md` 중 적용되는
파일에 다음 공용 지침을 추가한다.

```markdown
## Sanho workflow

This repository uses Sanho to synchronize its `docs/` directory with the canonical docs repository.

- At the start of a task and before any authorized commit or push, run `sanho status --json`. If it fails, report the error and do not bypass Sanho.
- If the repository is not initialized, stop and ask the user for the project name, docs repository URL, and any non-default socket path. Do not guess these values or initialize the workspace on your own.
- Edit `docs/` as normal workspace files, but use normal Git commands and let the installed Sanho hooks run. Sanho does not grant permission to commit or push.
- Never bypass Sanho with `--no-verify`, a force push used to evade a Sanho block, a `sanho push` command (it does not exist), or manual edits or removal of `.sanho_docs_hash`, `.git/sanho`, Git operation metadata, or Sanho-owned hook entries.
- Do not run `sanho clean`, `sanho init --force`, or `sanho pull --force` without explicit user approval.
- When Sanho interrupts a commit or push, inspect both `git status` and `sanho status --json`. Rerun the same command only when Sanho explicitly instructs you to; for pending main publication, follow the reported normal Git push sequence and then retry the original push.
- For conflicts or an existing pull-commit transaction, use `pull_commit.classification`, `reason`, and `next_command`. For an active Git operation, treat `git_operation.next_commands` as choices, not commands to execute automatically. Do not choose continue, abort, or quit, delete metadata, or discard work without confirming the user's intent.
```

## 충돌 방지 원칙

daemon은 같은 `docs_repo_id`에 대한 sync, HEAD 조회, snapshot 조회, push,
삭제를 직렬화한다. push를 시작할 때 origin을 fetch하고 로컬 clone을
원격 기본 브랜치로 되돌린 다음, 요청의 base commit과 현재 HEAD를 비교한다.

- base가 같으면 snapshot을 commit하고 origin에 push한다.
- base가 다르면 `outdated`로 응답하며 원격을 덮어쓰지 않는다.
- state 저장이나 push가 실패하면 clone을 origin 상태로 복구한다.
- HEAD를 확인할 수 없으면 일부 결과를 추측해 반환하지 않고 요청을 실패시킨다.

따라서 애플리케이션 저장소의 `docs/`는 독립된 진실의 원천이 아니라
전용 docs 저장소의 특정 commit을 기준으로 한 작업 사본이다.

## 테스트

```bash
make test
```

`make test`는 `test-prepare`, `test-unit`, `test-int`, `test-e2e`를
순서대로 실행한다. 각 단계는 따로 실행할 수 있고, `-daemon` 또는
`-client` 접미사가 붙은 target으로 범위를 좁힐 수도 있다.

구조와 운영 절차는 [아키텍처](../architecture.md)와
[운영 가이드](../operations.md)를 참고한다.
