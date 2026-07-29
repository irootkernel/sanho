# Kkachi 운영 가이드

## 빌드와 실행

```bash
make server-build
make cli-build
make server-run
```

개발 중에는 `make server-run-dev`로 `go run`을 사용할 수 있다. daemon은
별도의 frontend build나 hot-reload 도구를 요구하지 않는다.

환경 변수는 두 개다.

| 변수 | 기본값 | 설명 |
|---|---|---|
| `PORT` | `5789` | HTTP listen port |
| `STATE_FILE_PATH` | `data/kkachi_state.json` | daemon state JSON 경로 |

docs clone은 프로젝트 등록 시 `data/docs_repos/<docs_repo_id>` 아래에
생성된다. `data/`, `tmp/`, `.kkachi*`와 인증 정보는 commit하지 않는다.

## macOS LaunchAgent

설치 전에 현재 checkout의 GitHub SSH 접근이 가능한지 확인한다.

```bash
make check-github-ssh
make install-launchagent
make status-launchagent
```

설치 target은 현재 소스에서 `bin/server`를 다시 빌드하고
`run-kkachi.sh`를 직접 실행하도록 plist를 설치한다. `run-kkachi.sh`는
Node나 shell session manager를 시작하지 않는다.

로그:

```text
~/Library/Logs/kkachi/kkachi.out.log
~/Library/Logs/kkachi/kkachi.err.log
```

제거:

```bash
make uninstall-launchagent
```

## 상태 확인

```bash
curl --fail http://127.0.0.1:5789/healthz
kkachi-cli state --all --server-url http://127.0.0.1:5789
kkachi-cli state --all --server-url http://127.0.0.1:5789 --json
```

정상 health 응답은 `{"ok":true}`다. `/state`는 등록된 모든 프로젝트의
원격 HEAD를 갱신하므로 Git fetch나 SSH 오류가 있으면 전체 요청이
실패한다. 일부 프로젝트만 stale 값으로 표시하지 않는 의도적인 정책이다.

## 장애 대응

### `docs_repo_busy`

같은 docs repo에서 다른 sync, read, push, delete가 진행 중이다. 진행 중인
작업이 끝난 뒤 다시 시도한다. 잠금을 우회해 clone을 직접 수정하지 않는다.

### `outdated`

다른 작업공간이 먼저 docs origin을 갱신했다. Git commit 중이라면
pre-commit hook이 `pull-commit` 흐름을 자동 실행한다. 충돌이 없으면 원격
docs만 담은 `[KKACHI] Update docs` 커밋을 최신 `main` 위에 만들고 현재
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
원격 애플리케이션 저장소에는 자동 push하지 않는다.

충돌이 있으면 파일을 해결하고 stage한 뒤
`kkachi-cli pull-commit --continue`를 실행한다. 시스템 커밋 생성 전에는
`kkachi-cli pull-commit --abort`로 원래 상태를 복원할 수 있다. 원격
history를 강제 push로 덮어쓰지 않는다.

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
`pull-commit`이 이 기준선을 소비해 `[KKACHI] Update docs` commit을 만든다.
pull로 새로 생긴 원격 파일은 사용자가 실제로 stage해 삭제하지 않는 한
로컬 staged 삭제로 해석하지 않는다. `--force` pull은 기존 staged docs도
버리므로 기준선의 원래 index 역시 현재 HEAD 기준으로 다시 잡는다.

실제 `git pull`, rebase/amend, branch checkout처럼 HEAD가 바뀌는 작업은
각각 `post-merge`, `post-rewrite`, `post-checkout` hook에서 HEAD의 docs
tree를 다시 확인한다. 현재 tree와 일치하는 reachable `docs-version`
commit을 찾았을 때만 `.kkachi_docs_hash`와 daemon workspace hash를 함께
갱신한다. 일치하는 commit이 없거나 pending pull 위에서 dirty docs가
발견되면 추측해서 보고하지 않고 경고 또는 보호 실패로 남긴다.

### Git fetch 또는 push 실패

SSH key, 저장소 URL, 네트워크, branch 권한을 확인한다.

```bash
git ls-remote <docs-repo-url> HEAD
```

daemon은 실패 후 clone을 origin으로 reset한다. 장애 조사 중에도
`data/docs_repos`의 clone에서 임의 commit이나 force push를 만들지 않는다.

### state 손상

primary state 파일이 깨졌다면 daemon은 `<state-path>.bak`에서 자동
복구한다. 둘 다 깨졌을 때는 시작이 실패한다. 이 경우 두 파일을 먼저
별도 위치에 보존하고 JSON과 로그를 조사한다. 빈 state 파일로 덮어써서
문제를 숨기지 않는다.

## 검증

```bash
make server-test-prepare
make server-test-unit
make server-test-integration
make server-test-e2e

make cli-test-prepare
make cli-test-unit
make cli-test-integration
make cli-test-e2e
```

server와 CLI E2E는 기본적으로 임의 loopback port와 임시 state를 사용하는
독립 daemon을 띄운다. 실행 중인 별도 daemon을 대상으로 확인하려는
경우에만 `E2E_BASE_URL`을 명시한다.

```bash
make server-test-e2e
make server-test-e2e E2E_BASE_URL=http://127.0.0.1:5789
make cli-test-e2e
make cli-test-e2e E2E_BASE_URL=http://127.0.0.1:5789
```

통합·E2E 테스트의 docs 저장소에는 실제 운영 저장소 대신 `/tmp` 아래의
폐기 가능한 bare Git 저장소와 clone을 사용한다. 전체 검증은
`make test-all`로 실행한다.
