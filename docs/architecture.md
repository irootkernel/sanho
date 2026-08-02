# Sanho 아키텍처

## 제품 경계

Sanho의 책임은 전용 docs 저장소를 단일 진실의 원천으로 유지하고,
애플리케이션 저장소의 `docs/` 작업 사본이 어느 commit을 기준으로 하는지
추적하는 것이다.

실행 구성요소는 `sanhod`와 `sanho` CLI뿐이다. daemon은 셸 명령을
사용자 대신 실행하거나, 터미널·PTY·에이전트 세션을 만들거나, Web UI를
제공하지 않는다.

## 구성

```text
application repo
  docs/ + .sanho.json + Git hooks
             |
             | HTTP over Unix socket
             v
       sanhod
       | ~/.sanho/state.json
       | repo coordinator
       v
  ~/.sanho/docs_repos/* <----> canonical docs origin
```

- CLI는 로컬 docs snapshot 생성, 병합, 상태 표시, hook 연동을 담당한다.
- daemon은 프로젝트·작업공간 등록, snapshot 제공, 조건부 push를 담당한다.
- docs origin의 commit history가 문서 내용의 최종 기록이다.
- daemon state는 프로젝트 매핑과 각 작업공간의 기준 commit을 저장한다.

## 애플리케이션 main 게시 계약

CLI가 만든 docs system commit은 Git common directory 아래의 private
metadata에 기록한다. linked worktree도 같은 게시 대기 상태를 공유한다.
상태에는 Sanho가 만든 commit OID, parent, docs hash와 제목을 저장하며,
pre-push는 commit graph와 trailer를 다시 검증한다. 제목 prefix만으로 사용자
commit을 system commit으로 분류하지 않는다.

origin branch push에 `origin/main` update가 포함되면 원래 push가 로컬 main
전체를 게시한다. 포함되지 않으면 CLI가 먼저 `refs/heads/main`을
`refs/heads/main`으로 push하고, 성공 및 원격 도달을 확인한 뒤 target push를
계속한다. 이 선행 push도 설치된 pre-push hook을 실행하지만 remote main
update 분기로 들어가므로 재귀 push를 만들지 않는다.

모든 main update는 fast-forward여야 한다. 원격 경합이나 branch protection
실패는 target push까지 차단하며 force push로 복구하지 않는다. 다른 remote,
tag-only push와 ref 삭제는 origin main 게시를 유발하지 않는다.

## 동시성 계약

하나의 daemon 프로세스에서 모든 Git 작업은 `docs_repo_id`별 coordinator를
공유한다. 같은 repo의 sync, read, push, delete는 동시에 clone을 만지지
않는다. 서로 다른 repo는 병렬로 처리할 수 있다.

push는 기다리지 않고 잠금 획득을 시도한다. 이미 같은 repo에서 작업 중이면
`docs_repo_busy`로 실패하므로 호출자는 잠시 후 다시 시도할 수 있다. 일반
조회와 관리 작업은 context가 취소될 때까지 잠금을 기다린다.

## 원격 HEAD 계약

daemon은 로컬 clone의 기존 HEAD를 그대로 신뢰하지 않는다. HEAD 또는
snapshot을 읽기 전에 다음 순서로 clone을 갱신한다.

1. origin fetch
2. `main` 또는 `master` checkout
3. `origin/main` 또는 `origin/master`로 hard reset

갱신에 실패하면 stale HEAD를 반환하지 않는다.

## 프로젝트 상태 비교 계약

`GET /projects/{project}/status`는 호출자가 전달한 `workspace_id`와 로컬
`docs_hash`를 기준으로 같은 프로젝트의 작업공간을 비교한다. daemon은
작업공간이 해당 프로젝트에 속하는지 확인한 뒤 docs repo 잠금을 한 번
획득하고 clone도 한 번만 갱신한다.

각 commit은 Git commit object로 먼저 resolve한다. 기준 commit을 찾을 수
없으면 요청 전체를 `unknown_docs_commit`으로 실패시킨다. 다른 작업공간의
저장된 commit만 찾을 수 없다면 그 행을 `unknown`으로 반환하고 나머지
비교는 유지한다. 알려진 commit끼리는 `git rev-list --left-right --count`로
`same`, `ahead`, `behind`, `diverged` 관계와 양쪽 commit 수를 계산한다.

## 문서 push 계약

문서 snapshot을 게시하는 동안 같은 repo의 잠금을 끝까지 유지한다.

1. clone을 origin으로 갱신한다.
2. 요청의 base commit이 현재 원격 HEAD와 같은지 확인한다.
3. 다르면 `outdated`를 반환한다.
4. 같으면 snapshot을 적용하고 commit한다.
5. 작업공간의 기준 hash를 새 commit으로 저장한다.
6. 새 commit을 origin에 push한다.

5단계가 실패하면 로컬 commit을 버리고 origin으로 복구한다. 6단계가
실패하면 작업공간 hash를 이전 값으로 되돌리고 clone도 origin으로
복구한다. 따라서 push되지 않은 로컬 commit이 이후 HEAD 조회에 노출되지
않는다.

## state 내구성

state 변경은 메모리의 복사본에 먼저 적용한다. 디스크 저장이 실패하면
메모리도 이전 상태로 되돌린다.

저장은 state 파일과 `.bak` 각각에 대해 같은 디렉터리의 임시 파일을
사용한다. 파일을 쓰고 `fsync`한 다음 atomic rename하고 디렉터리도
`fsync`한다. 시작 시 primary JSON이 없거나 손상됐고 정상 백업이 있으면
백업을 primary로 복원한다. 둘 다 손상됐으면 daemon은 빈 상태를 만들어
계속하지 않고 시작에 실패한다.

## HTTP 인터페이스

HTTP는 TCP port를 열지 않고 기본 `~/.sanho/sanhod.sock` Unix socket을
통해서만 전달한다. CLI는 `.sanho.json`의 절대 `socket_path`를 사용하며,
전역 `--socket`, `SANHO_SOCKET`, `SANHO_HOME`으로 기본 경로를 바꿀 수 있다.

| Method | Path | 용도 |
|---|---|---|
| `GET` | `/healthz` | daemon 생존 확인 |
| `POST` | `/projects` | 프로젝트와 docs repo 등록 |
| `DELETE` | `/projects/{project}` | 프로젝트 삭제 |
| `GET` | `/projects/{project}/status` | 로컬 docs 기준과 프로젝트 작업공간 비교 |
| `POST` | `/workspaces/register` | 작업공간 등록 |
| `DELETE` | `/workspaces/{workspace_id}` | 작업공간 등록 해제 |
| `GET` | `/docs/head` | 원격 기준 docs HEAD 조회 |
| `GET` | `/docs/snapshot` | 특정 commit snapshot 조회 |
| `POST` | `/docs/push` | base 조건을 검사해 snapshot 게시 |
| `GET` | `/state` | 프로젝트 HEAD와 작업공간 상태 조회 |

정의되지 않은 경로는 JSON `404 not_found`를 반환한다. 정적 파일, Swagger,
OpenAPI 문서, WebSocket endpoint는 없다.
