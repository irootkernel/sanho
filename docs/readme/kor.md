# Kkachi

Kkachi는 여러 애플리케이션 저장소의 `docs/` 디렉터리를 전용 docs
저장소와 동기화하고, 여러 작업공간이 동시에 문서를 갱신할 때 생기는
충돌을 막는 도구다.

제품은 두 실행 파일만 사용한다.

- `kkachi-server`: docs 저장소의 Git 접근과 작업공간 상태를 조정하는 HTTP daemon
- `kkachi`: 개발자와 Git hook이 사용하는 CLI

Web UI, 브라우저 터미널, PTY, 세션 실행 기능은 제공하지 않는다.

## 빠른 시작

필수 환경은 Go 1.25 이상과 Git이다. docs 저장소를 읽고 쓸 수 있는 SSH
인증도 준비해야 한다. Node.js와 npm은 필요하지 않다.

```bash
make server-build
make cli-build
make server-run
```

기본 서버 주소는 `http://127.0.0.1:5789`이고 state 파일은
`data/kkachi_state.json`에 저장된다.

애플리케이션 Git 저장소에서 다음 명령으로 작업공간을 초기화한다.

```bash
kkachi init \
  --server-url http://127.0.0.1:5789 \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

`init`은 현재 docs snapshot을 내려받고 `.kkachi.json`,
`.kkachi_docs_hash`를 만든 뒤 문서 동기화용 Git hook을 설치한다.

## 일상 작업

```bash
kkachi status  # 로컬 기준과 HEAD, 같은 프로젝트 작업공간 비교
kkachi status --json  # 자동화 도구용 구조화 상태
kkachi pull    # 최신 docs snapshot 반영
kkachi fix     # 충돌을 직접 해결한 뒤 merge 결과 게시
kkachi state   # 등록된 프로젝트와 작업공간 상태 조회
kkachi state --json   # 현재 프로젝트 상태를 JSON으로 조회
```

`kkachi status`는 현재 작업공간의 전체 docs commit과 HEAD를 보여주고,
같은 프로젝트에 등록된 모든 작업공간을 현재 로컬
`.kkachi_docs_hash` 기준으로 비교한다. 관계는 다음과 같다.

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

로컬 docs 변경이 있을 때 `kkachi pull`로 덮어쓰려면 명시적으로
`--force`를 사용해야 한다. 설정만 제거하려면 `kkachi clean`을 사용하고,
docs 디렉터리까지 지우려면 `--remove-docs`를 별도로 지정한다.

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
make server-test
make cli-test
# 전체:
make test-all
```

구조와 운영 절차는 [아키텍처](../architecture.md)와
[운영 가이드](../operations.md)를 참고한다.
