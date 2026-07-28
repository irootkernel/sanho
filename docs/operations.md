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
kkachi state --all --server-url http://127.0.0.1:5789
```

정상 health 응답은 `{"ok":true}`다. `/state`는 등록된 모든 프로젝트의
원격 HEAD를 갱신하므로 Git fetch나 SSH 오류가 있으면 전체 요청이
실패한다. 일부 프로젝트만 stale 값으로 표시하지 않는 의도적인 정책이다.

## 장애 대응

### `docs_repo_busy`

같은 docs repo에서 다른 sync, read, push, delete가 진행 중이다. 진행 중인
작업이 끝난 뒤 다시 시도한다. 잠금을 우회해 clone을 직접 수정하지 않는다.

### `outdated`

다른 작업공간이 먼저 docs origin을 갱신했다. `kkachi pull` 또는 hook이
안내하는 병합 절차로 최신 문서를 반영하고, 충돌을 해결한 뒤 `kkachi fix`를
실행한다. 강제 push로 원격 history를 덮어쓰지 않는다.

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

server E2E는 기본적으로 임의 loopback port와 임시 state를 사용하는 독립
daemon을 띄운다. 실행 중인 운영 daemon을 대상으로 확인하려는 경우에만
`E2E_BASE_URL`을 명시한다.

```bash
make server-test-e2e
make server-test-e2e E2E_BASE_URL=http://127.0.0.1:5789
```

통합·E2E 테스트의 docs 저장소에는 실제 운영 저장소 대신 `/tmp` 아래의
폐기 가능한 bare Git 저장소와 clone을 사용한다. 전체 검증은
`make test-all`로 실행한다.
