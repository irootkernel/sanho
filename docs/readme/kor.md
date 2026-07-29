# Sanho

Sanho는 여러 애플리케이션 저장소의 `docs/` 디렉터리를 전용 docs
저장소와 동기화하고, 여러 작업공간이 동시에 문서를 갱신할 때 생기는
충돌을 막는 도구다.

제품은 두 실행 파일만 사용한다.

- `sanhod`: docs 저장소의 Git 접근과 작업공간 상태를 조정하는 HTTP daemon
- `sanho`: 개발자와 Git hook이 사용하는 CLI

Web UI, 브라우저 터미널, PTY, 세션 실행 기능은 제공하지 않는다.

## 빠른 시작

필수 환경은 Go 1.25 이상과 Git이다. docs 저장소를 읽고 쓸 수 있는 SSH
인증도 준비해야 한다. Node.js와 npm은 필요하지 않다.

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

로컬 docs 변경이 있을 때 `sanho pull`로 덮어쓰려면 명시적으로
`--force`를 사용해야 한다. 설정만 제거하려면 `sanho clean`을 사용하고,
docs 디렉터리까지 지우려면 `--remove-docs`를 별도로 지정한다.

Git commit 시 중앙 docs가 갱신된 상태라면 pre-commit hook이
`pull-commit` 흐름을 자동으로 실행한다. 첫 시도에서는 `origin/main`을
fetch하고, 원격 docs만 담은 `[SANHO] Update docs` 커밋을 최신 `main` 위에
만든다. 현재 branch가 `main`이면 branch를 유지하고, unpublished linear
feature branch라면 system commit 위로 local commit을 rebase한다. published
branch, merge commit이 있는 branch, 갈라진 local/remote `main`은 자동으로
history를 바꾸지 않고 실패한다. 성공 시 현재 staged/unstaged 변경을
보존한 뒤 원래 commit을 중단한다. 같은 `git commit` 명령을 다시 실행하면
보존한 staged 변경을 Git의 commit index에 복원해 원래 커밋을 계속한다.

같은 위치를 함께 수정했다면 원격을 덮어쓰지 않고 충돌 상태로 멈춘다.
파일을 해결하고 stage한 뒤 `sanho pull-commit --continue`를 실행한다.
시스템 커밋이 만들어지기 전이라면 `sanho pull-commit --abort`로
원래 staged/unstaged docs 상태를 복원할 수 있다. 시스템 커밋 제목은
`.sanho.json`의 `docs_sync_commit_message`로 바꿀 수 있으며 기본값은
`[SANHO] Update docs`다.

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

## 충돌 방지 원칙

서버는 같은 `docs_repo_id`에 대한 sync, HEAD 조회, snapshot 조회, push,
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
make daemon-test
make cli-test
# 전체:
make test-all
```

구조와 운영 절차는 [아키텍처](../architecture.md)와
[운영 가이드](../operations.md)를 참고한다.
