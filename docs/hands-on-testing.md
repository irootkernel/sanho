# Sanho hands-on 테스트

## 목적과 원칙

이 문서는 자동 테스트가 재현하기 어려운 경계를 릴리스 전에 빠뜨리지 않고
확인하기 위한 체크리스트다. 각 항목은 자동 테스트를 대체하지 않으며
`make test`가 통과한 release candidate로만 수행한다.

v0.2에서 자동 스위트가 이미 덮는 영역은 여기에서 다루지 않는다.
`test/cli/integration`은 실제 git 저장소에서 init, commit stamp, 오프라인
commit, sync 충돌 → 해소 → push 성공 경로, `clean --dry-run` 무변경,
`doctor --fix`, migrate와 v0.1 강등까지 실행한다. `test/docsync`는 3-way 병합,
빈 canonical, legacy base, rewrite 재유도, dirty 거절을 실제 git으로 검사한다.
따라서 hands-on의 초점은 **자동화가 만들 수 없는 것**이다. 실제 hosting과
SSH 인증, 서버 측 branch 보호 규칙, 물리적으로 다른 두 머신, 실사용 규모의
저장소, 그리고 진짜 v0.1 설치본에서 출발하는 migration.

- 실제 저장소를 사용할 때는 대상과 허용된 write 범위를 먼저 기록한다.
- force push, hook 우회(`--no-verify`), `.git/sanho` 수동 삭제는 사용하지
  않는다. negative fixture를 만들 때만 예외이며 그 사실을 기록한다.
- 검증용 branch와 파일 이름에는 날짜와 실행 ID를 넣어 기존 작업과 충돌하지
  않게 한다.
- 실패도 유효한 증거다. branch rule이나 network 실패를 우회하지 말고 출력과
  전후 SHA를 기록한다.
- Sanho 저장소의 push, tag, release, install은 별도 승인이 있을 때만 한다.

## 공통 실행 기록

테스트 실행마다 아래 양식을 복사해 채운다.

```text
실행 ID:
실행 시각 / 테스트 담당자:
OS / Git / Go 버전:
sanho version --json:
sanho 절대 경로 (command -v):
SANHO_HOME (기본 ~/.sanho 여부):
대상 저장소와 허용된 write 범위:
시작 canonical head SHA:
시작 local HEAD / branch / git status --porcelain=v1:
시작 sanho status --json:
시작 .sanho_base.json:
예상 결과:
실제 명령과 종료 코드:
실제 결과와 주요 출력:
종료 canonical head SHA:
종료 sanho status --json / .sanho_base.json:
정리한 branch, clone, 임시 경로:
남겨 둔 검증 파일과 이유:
판정: PASS / FAIL / BLOCKED
```

공통 사전 점검은 다음과 같다.

```bash
command -v sanho
sanho version --json
git --version
git status --porcelain=v1
git remote -v
sanho status --json
sanho doctor --json
```

실행 중에는 중요한 단계마다 local HEAD, canonical head, `.sanho_base.json`,
`sanho status --json`을 다시 기록한다. 끝나면 임시 clone과 검증 branch를
정리하되, 유지하기로 한 validation 파일과 그 commit SHA는 실행 기록에 남긴다.

canonical head는 다음으로 읽는다.

```bash
git ls-remote --heads <docs-repo-url> refs/heads/main
# 또는 작업공간의 private clone에서
CLONE="$(git rev-parse --path-format=absolute --git-common-dir)/sanho/canonical"
git -C "$CLONE" log --oneline -5 refs/remotes/origin/main
```

## v0.1 체크리스트 처분

| v0.1 항목 | 처분 | v0.2 대응 |
|---|---|---|
| H01 실제 세 저장소 양방향 동기화와 main 게시 | **변형** | H01. 애플리케이션 `main` 선행 게시 부분은 은퇴한다. |
| H02 direct URL·alias remote 우회 차단 | **은퇴** | 애플리케이션 main 게시 계약이 없다. pre-push는 어떤 remote로 push하든 canonical 게시만 판단한다. |
| H03 실제 branch rule에 의한 main 거부 | **변형** | H03. 대상이 애플리케이션 `origin/main`이 아니라 **canonical docs 저장소**의 게시 branch다. |
| H04 main 성공 후 target push 실패와 재시도 | **은퇴** | 두 단계 push가 없다. 게시는 canonical로 한 번 나가고, 애플리케이션 push는 git이 그대로 수행한다. |
| H05 remote main 경합과 divergence | **변형** | H02. canonical CAS 경합으로 대체하며 두 머신에서 수행한다. |
| H06 v0.1.2 legacy v2 중단 복구 | **은퇴** | transaction engine과 5단계 상태가 없다. 남을 수 있는 상태는 충돌 sync 하나이며 자동 스위트가 덮는다. |
| H07 linked worktree queue와 동시 실행 | **변형** | H07. 검사 대상이 게시 대기 queue가 아니라 **공유 private clone**이다. |
| H08 custom hooksPath와 legacy hook 갱신 | **변형** | H08. hook 자동 upgrade·재시도 요구 계약은 은퇴하고, 정확한 줄 일치 공존만 확인한다. |
| H09 SSH·network 실패와 재시도 | **유지** | H09. 읽기/쓰기 비대칭 확인이 추가된다. |
| H10 launchd/systemd 재시작과 업그레이드 | **은퇴** | daemon이 없다. |
| H11 clean stale Git operation 차단과 worktree 격리 | **은퇴** | Sanho는 Git operation metadata를 검사하지 않는다. rebase 중에도 hook은 exit 0이다. |
| H12 rebase lifecycle 무변경과 종료 후 수렴 | **은퇴** | 위와 같은 이유. base 재유도는 자동 스위트가 덮는다. |
| H13 pre-push docs provenance 무결성 | **은퇴** | trailer는 gate 입력이 아니다. 게시 판정은 base 파일과 tree 비교로 한다. 마커 게이트는 자동 스위트가 덮는다. |
| H14 설치 hook trust boundary와 status 호환성 | **변형** | H12. status JSON 호환성 부분은 은퇴한다(스키마가 새로 정의됐다). |

신설 항목은 H04(오프라인 경계), H05(대형 저장소 체감), H06(실사용 migrate),
H10(canonical rewrite 복구), H11(symlink·mode·binary 왕복)이다.

## H01. 실제 원격 세 저장소 양방향 전파 (변형)

로컬 bare remote로는 인증, Git hosting, 실제 전파를 증명할 수 없다.

1. 실제 `sanho-server`, `sanho-client`, `sanho-docs`를 사용한다. 세 저장소의
   시작 SHA와 clean status를 기록하고, 새 `sanho` binary와 격리된 clone을
   사용한다.
2. 두 애플리케이션 저장소를 `sanho init`으로 온보딩한다. fresh 모드가 canonical
   docs를 index에 올리고 사용자가 commit하는지 확인한다.
3. 저장소 A에서 docs를 바꾸고 `git commit` → `git push`한다.
   `sanho: published docs <oid> (fast_forward)`가 출력되고, canonical head가
   실제로 그 내용으로 움직였는지 `git ls-remote`로 확인한다.
4. canonical commit의 제목이 `docs: <repo>/<branch> (<N> app commits)`이고,
   본문에 `source: <workspace-id> @ <tip>`과 `commits:` 목록이 있는지 확인한다.
5. 저장소 B에서 `sanho status --refresh`가 behind를 보고하는지 확인하고
   `sanho sync`로 받는다. 만들어진 commit의 제목이
   `docs: sync to <oid12>`이고 author가 **사용자**인지 확인한다.
   `[SANHO]` 문자열이 어디에도 없어야 한다.
6. 저장소 B에서 docs를 바꿔 push하고, A가 다시 받는지 확인한다.
7. 코드만 바꾼 commit과 push가 canonical에 아무 commit도 만들지 않는지
   확인한다(case `up_to_date`).
8. tag push와 branch 삭제가 게시 판단 없이 그대로 통과하는지 확인한다.
9. 세 저장소의 최종 SHA와 두 작업공간의 `.sanho_base.json`을 기록한다.

애플리케이션 `main`을 Sanho가 자동으로 fast-forward하거나 push하면 FAIL이다.
v0.2는 애플리케이션 ref를 절대 움직이지 않는다.

## H02. 두 머신 동시 게시와 CAS 경합 (신설)

단일 머신 자동 테스트로는 "다른 머신의 게시자"를 만들 수 없다.

1. 물리적으로 다른 두 머신(또는 완전히 분리된 두 사용자 계정)에 v0.2를
   설치하고 같은 프로젝트의 애플리케이션 저장소를 각각 clone·init한다.
2. 두 머신에서 **서로 다른 docs 파일**을 각각 편집해 commit한다.
3. 두 머신에서 가능한 한 동시에 `git push`한다.
4. canonical에 정확히 두 개의 게시 commit이 선형으로 쌓였는지 확인한다. merge
   commit이 생기면 FAIL이다.
5. 진 쪽이 `auto_merge`로 자동 병합해 게시했는지, 아니면 재시도 후 성공했는지
   출력으로 확인한다. `sanho: docs must be reconciled before publishing
   (cas_retry_exhausted; …)`가 나왔다면 그것도 정상 결과이며, 안내대로
   `sanho sync` 후 재push가 성공하는지 확인한다.
6. 두 머신에서 **같은 docs 파일의 같은 줄**을 편집해 3~5단계를 반복한다. 진
   쪽이 `sanho: your docs changes conflict with upstream (base … → …)`로 거절되고
   원격 ref가 하나도 바뀌지 않는지 확인한다.
7. 진 쪽에서 `sanho sync` → 해소 → commit → push가 성공하는지 확인한다.
8. 각 머신의 `sanho status`가 상대를 sibling으로 보지 **못하는 것**이 정상임을
   기록한다. 레지스트리는 머신마다 독립이며 v0.2는 머신 간 sibling 가시성을
   제공하지 않는다(v0.1도 마찬가지였다).

어느 단계에서든 force push가 사용되거나 canonical 이력이 비선형이 되면 FAIL이다.

## H03. canonical 저장소의 branch 보호 규칙 (변형)

hosting service의 보호 규칙과 사용자 권한은 로컬 bare remote가 대신할 수 없다.

1. canonical docs 저장소의 게시 branch(`main`)에 직접 push를 금지하는 보호
   규칙을 승인된 test repository에 설정한다.
2. 애플리케이션 저장소에서 docs를 바꿔 commit하고 `git push`한다.
3. 게시가 거부되고, 애플리케이션 push도 함께 중단되며, 애플리케이션 원격 ref가
   바뀌지 않는지 확인한다.
4. 출력이 `sanho: canonical repository unreachable (<url>): <원인>` 형태로
   원인 줄과 조치 줄을 감싸는지, raw Go 오류 체인이 노출되지 않는지 확인한다.
5. force push나 우회가 시도되지 않았는지, canonical head가 그대로인지 확인한다.
6. 규칙을 정상 절차로 충족(또는 해제)한 뒤 **같은 `git push`**를 재시도해
   성공하는지 확인한다.
7. 별도로, 게시 branch가 `master`뿐인 저장소를 하나 준비해 `sanho init`이
   `branch master`로 해석하는지 확인한다.

## H04. 오프라인 경계 — commit은 되고 push는 안 된다 (신설)

v0.1의 Critical C1은 daemon이 없으면 모든 commit이 막히는 것이었다. 실제 network
차단으로 확인한다.

1. 정상 작업공간을 만들고 `sanho status --refresh`로 캐시를 채운다.
2. 승인된 방법으로 network를 끊는다(Wi-Fi off, 방화벽 규칙 등).
3. 다음이 모두 성공하는지 확인한다.
   - 코드만 바꾼 `git commit`
   - docs를 바꾼 `git commit` (trailer가 그대로 stamp되는지도 확인)
   - `git commit --amend`
   - `git checkout -b`, `git rebase` 같은 HEAD 이동
   - `sanho status`, `sanho state`, `sanho doctor`
4. `sanho status`의 `data` 줄이 캐시 나이를 말하는지 확인한다. 24시간 이상이면
   `run 'sanho status --refresh'`를 함께 말해야 한다.
5. 다음이 명확한 이유와 함께 실패하는지 확인한다.
   - `git push` → `sanho: canonical repository unreachable (…)` +
     `error: push rejected — no remote ref was changed`
   - `sanho sync`, `sanho pull`, `sanho status --refresh`
6. network를 복구하고 같은 `git push`가 별도 상태 정리 없이 성공하는지
   확인한다.
7. 오프라인 구간에서 만든 commit이 전부 canonical에 게시되는지 확인한다.

오프라인 상태에서 `git commit`이 한 번이라도 실패하면 FAIL이다.

## H05. 대형 docs 저장소 체감 (신설)

자동 테스트의 fixture는 파일 몇 개다. 실사용 규모에서 병합 예측 비용과 hook
지연을 사람이 직접 느껴 봐야 한다.

1. 실사용 규모(권장: 파일 1,000개 이상, 이력 500 commit 이상, 총 50 MB 이상)의
   docs 저장소를 준비하거나 승인된 실제 저장소를 사용한다.
2. `sanho init` 소요 시간을 측정하고 기록한다(clone + 첫 fetch 포함).
3. docs를 건드리지 않는 commit을 10회 하고 `pre-commit` 지연을 측정한다.
   체감 지연이 있으면 기록한다.
4. canonical을 앞서게 만든 뒤 docs를 건드리지 않는 commit을 다시 10회 한다.
   이때는 병합 예측이 실제로 실행된다. 지연을 측정해 3단계와 비교한다.
5. 병합 예측 비용이 눈에 띄면(체감 1초 이상) 기록한다. 설계상 이 경우
   경고를 behind 개수만 말하도록 낮추는 선택지가 열려 있으므로, 측정치가
   그 판단의 근거가 된다.
6. `git push` 게시 시간과 `sanho sync` 시간을 측정한다.
7. `du -sh .git/sanho/canonical`로 private clone 크기를 기록한다. 작업공간마다
   하나씩 생기므로 디스크 비용이 사용자에게 보인다.
8. docs를 바꿔 게시하는 push를 충분히 반복한 뒤 같은 크기를 다시 기록한다.
   실제 게시 뒤에는 `git gc --auto --quiet`가 best-effort로 실행되지만, Git이
   자체 임계값에 따라 아무 작업도 하지 않을 수 있다. 일반 출력에는 gc 진단이
   없어야 하고, gc가 실패해도 애플리케이션 push는 성공해야 한다.
9. `sanho status`(캐시)와 `sanho status --refresh`의 시간 차이를 기록한다.

이 항목은 PASS/FAIL보다 **측정치 기록**이 목적이다. 임계값을 넘는 항목은
BLOCKED이 아니라 잔여 위험으로 명시한다.

## H06. 실사용 v0.1 → v0.2 migration (신설)

자동 스위트는 합성된 v0.1 작업공간을 쓴다. 진짜 v0.1 설치본에서 출발해야
확인되는 것들이 있다.

1. 실제로 v0.1을 사용해 온 머신(또는 그 완전한 복제본)을 대상으로 한다.
   `~/.sanho/state.json`, 각 작업공간의 `.sanho.json`, `.sanho_docs_hash`,
   hook 파일 내용과 mode를 모두 백업하고 기록한다.
2. daemon이 실행 중인 상태에서 시작한다. `sanhod --version`과 service 등록
   상태를 기록한다.
3. v0.1 binary로 진행 중인 transaction이 없는지 확인한다. 일부러 하나 만들어
   두고 `sanho migrate`가 거절하는지 확인한 뒤, v0.1로 정리한다.
   ```text
   sanho: a v0.1 pull-commit transaction or pending-fix state is still present; …
   ```
4. `~/.sanho/state.json`과 `.bak`을 **migration 전에** 별도 이름으로 복사한다.
   migrate가 이 두 파일을 v2 스키마로 덮어쓰기 때문이다.
5. v0.2 binary를 설치한다. daemon은 아직 그대로 둔다.
6. **강등 상태를 확인한다.** `git commit`이 성공하며 migrate 힌트를 출력하고,
   `git push`가 같은 힌트와 함께 막히고, `sanho status`가 exit 1로 거절하는지
   확인한다.
7. `sanho migrate`를 실행한다. 출력에 workspace, project, docs repo, clone,
   docs base, hook 수, backup 파일명이 나오고 daemon 정지 안내 3줄이
   출력되는지 확인한다.
8. `.sanho.json.bak`과 `.sanho_docs_hash.bak`이 생겼고, `.sanho_docs_hash`
   원본이 그대로 남아 있는지 확인한다.
9. `.sanho_base.json`의 commit이 원래 `.sanho_docs_hash` 값과 같고, tree가
   canonical에서 해석돼 채워졌는지 확인한다.
10. hook 파일에서 v0.1 7종 라인이 사라지고 v0.2 6종만 남았는지, custom 내용과
    permission bit가 보존됐는지 확인한다. `sanho doctor`가
    `all 6 hooks installed exactly once`를 보고해야 한다.
11. `sanho migrate`를 한 번 더 실행해 `sanho: already migrated`가 나오는지
    확인한다(멱등).
12. 안내된 명령으로 daemon을 정지·해제한다. 그 뒤 정상 흐름(commit → sync →
    push)을 한 바퀴 돌린다.
13. 여러 작업공간이 있으면 **하나씩** migrate하며, 아직 migrate하지 않은
    작업공간이 여전히 강등 상태로 안전하게 동작하는지 확인한다.
14. 별도 복제본에서 [복구 가이드](recovery.md#5-migrate-롤백)의 롤백 절차를
    끝까지 수행하고 v0.1이 정상 동작으로 되돌아오는지 확인한다.

migrate가 canonical 저장소를 조금이라도 바꾸면 FAIL이다.

## H07. linked worktree와 공유 clone (변형)

1. 하나의 애플리케이션 저장소에 `git worktree add`로 두 linked worktree를
   만든다.
2. 각 worktree에서 `sanho init`을 실행한다(또는 하나에서만 실행하고 다른
   쪽에서 상태를 본다).
3. 두 worktree가 **같은** private clone 경로를 쓰는지 확인한다.
   ```bash
   git rev-parse --path-format=absolute --git-common-dir
   ```
   `<common-dir>/sanho/canonical` 하나만 존재해야 한다.
4. hook이 `git rev-parse --git-path hooks`가 가리키는 공통 디렉터리에
   설치되고, worktree private gitdir에는 생기지 않는지 확인한다.
5. 한 worktree에서 충돌 sync를 만든다. `sync.json`이 **그 worktree의 private
   git dir**에만 생기고, 다른 worktree의 `sanho status`는
   `sync_in_progress: false`인지 확인한다.
6. 충돌 sync가 있는 worktree에서 push가 막히고, 다른 worktree에서는 push가
   정상 동작하는지 확인한다.
7. 두 worktree에서 거의 동시에 push를 실행한다. canonical이 선형으로 남고 두
   결과가 설명 가능한 순서로 끝나는지 확인한다.
8. 한 worktree에서 `sanho clean -y`를 실행한 뒤 다른 worktree의 상태를
   확인하고 기록한다(공유 clone이 사라지므로 재생성이 필요하다).

## H08. hook 소유권과 기존 hook 공존 (변형)

1. repository-local `core.hooksPath`와 사용자 전역 hooksPath를 각각 사용하는
   폐기 가능한 clone을 준비한다.
2. 기존 custom hook의 내용과 mode를 기록한 뒤 `sanho init`을 실행한다.
   custom `core.hooksPath`를 이름지어 거절하고 config, registry, clone, docs,
   hook을 한 글자도 바꾸지 않아야 한다.
3. v0.1 workspace에서 같은 설정으로 `sanho migrate`를 실행한다. `.bak`과
   v0.1 daemon state backup도 만들기 전에 거절하는지 확인한다.
4. 이미 초기화한 workspace의 설정을 custom hooksPath로 바꾸고 `sanho doctor
   --fix`를 실행한다. 경고만 출력하고 custom/default hook 모두 그대로여야 한다.
5. 기본 hooks 디렉터리의 마지막 유효 문장이 `exit 0`인 스크립트를 준비하고
   hook을 재설치한다. Sanho 라인이 `exit` **앞**에 삽입돼 실제로 실행되는지
   확인한다. `false; exit $?`와 일반 fall-through 실패도 Sanho가 성공으로
   덮어쓰지 않아야 한다.
6. `sanho hook pre-push`(무인자, v0.1 구형)와
   `sanho hook pre-push "$@"`가 한 파일에 함께 있는 상태를 만든다.
   `sanho doctor`가 v0.1 라인을 보고하고, `sanho doctor --fix` 뒤에는 정확히
   한 줄만 남는지 확인한다. 부분 문자열 매칭으로 둘이 서로를 가리면 FAIL이다.
7. `sanho clean -y` 후 Sanho 라인만 제거되고 custom 내용이 그대로 남는지
   확인한다.
8. shebang 한 줄과 Sanho 라인만 있던 hook 파일이 `clean` 후 **삭제**되는지
   확인한다. 빈 껍데기가 남으면 FAIL이다.

## H09. SSH·network 실패와 재시도 (유지)

1. 격리 환경에서 잘못된 SSH key, 끊긴 network, DNS 실패, 만료된 자격 증명 중
   승인된 방법으로 각각 실패를 만든다.
2. **읽기 경로**가 캐시로 계속 동작하는지 확인한다. `sanho status`,
   `sanho state`, `git commit`이 성공하고 데이터 나이를 명시해야 한다.
3. **쓰기 경로**가 fail-closed인지 확인한다. `sanho sync`, `sanho pull`,
   `git push`가 stale 성공으로 처리되지 않고 원인을 표시해야 한다.
4. passphrase가 걸린 key를 ssh-agent 없이 사용해 본다. **프롬프트가 뜨지 않고
   즉시 실패**하는지 확인한다(`BatchMode=yes`). hook이 사용자 입력을 기다리며
   멈추면 FAIL이다.
5. 도달 불가능한 host를 사용해 `ConnectTimeout` 안에 실패하는지 확인한다.
6. 실패 후 canonical 원격 ref와 애플리케이션 원격 ref가 모두 그대로인지
   확인한다.
7. 자격 증명 또는 network를 복구한 뒤 동일 명령을 별도 상태 삭제 없이
   재시도해 성공하는지 확인한다.
8. credential, SSH agent, proxy 설정을 원래대로 복구한다.

## H10. canonical 이력 rewrite 후 복구 (신설)

1. 폐기 가능한 canonical 저장소와 작업공간을 준비하고, docs를 몇 번 게시해
   이력을 만든다.
2. 작업공간에 로컬 docs 편집을 commit해 둔다. `.sanho_base.json`의 commit과
   tree를 기록한다.
3. canonical에서 승인된 방법으로 이력을 rewrite한다.
   - **case A — 내용 보존 rewrite**: 마지막 두 commit을 squash한다. 최종
     tree는 같다.
   - **case B — 내용 변경 rewrite**: base가 담고 있던 내용을 아예 없애는
     rebase를 한다.
4. case A에서 `git push`가 **사용자 개입 없이** 성공하는지 확인한다. Sanho가
   `docs-base-tree`로 자동 재유도한다.
5. case B에서 push가 다음 중 하나로 거절되는지 확인한다.
   - anchor를 찾은 경우: `Run 'sanho sync --rebase-onto <commit>' …`
   - 못 찾은 경우: `manual intervention required: no canonical commit carries
     this workspace's docs base tree.` + 후보 조회 명령
6. 안내된 명령을 그대로 실행해 성공하는지 확인한다. 안내가 실패하는 명령을
   지목하면 FAIL이다.
7. 후보 조회 명령(`git -C <clone> log --oneline refs/remotes/origin/<branch>`)을
   **출력된 그대로 복사해 실행**해 동작하는지 확인한다. 게시 branch가
   `master`인 저장소에서도 반복한다. 명령이 `origin/HEAD`를 지목하면
   private clone에는 그 ref가 없으므로 FAIL이다.
8. `sanho sync --rebase-onto <commit>` 후 해소 → commit → `sanho sync
   --continue` → push가 **추가 dummy commit 없이** 성공하고,
   `.sanho_base.json`이 새 canonical 상태를 가리키는지 확인한다. 해소 tip에서 새
   canonical 파일 하나를 누락한 변형은 absorption 증명을 통과하면 안 된다.
9. 존재하지 않는 commit을 `--rebase-onto`에 주면 거절되는지 확인한다.

## H11. symlink·file mode·binary 왕복 (신설)

v0.1은 tar snapshot 전송에서 symlink를 조용히 잃었다(audit H1). v0.2는 내용을
git object로 옮기므로 구조적으로 해결됐지만, 실제 파일 시스템에서 왕복을
확인한다.

1. docs 디렉터리에 다음을 만든다.
   - 상대 경로 symlink (`docs/latest.md -> ./v2.md`)
   - 디렉터리 symlink
   - 실행 비트가 있는 파일 (`docs/tools/build.sh`, mode `0755`)
   - 실제 binary 파일 (PNG 또는 PDF, 1 MB 이상)
   - 이름에 공백·따옴표·유니코드가 들어간 파일
   - 아주 긴 한 줄(1 MB 이상)로만 이루어진 텍스트 파일
2. commit하고 push한다. canonical에서 `git ls-tree -r` 결과의 mode와 type이
   그대로인지 확인한다(`120000` symlink, `100755` 실행 파일).
3. 다른 작업공간에서 `sanho sync` 또는 `sanho pull`로 받아 실제 파일 시스템에
   symlink가 symlink로, 실행 비트가 실행 비트로 복원되는지 확인한다.
4. binary 파일이 충돌 마커 오탐을 일으키지 않는지 확인한다(앞 8 KiB의 NUL로
   binary 판정).
5. 아주 긴 한 줄 파일 뒤에 진짜 충돌 마커를 넣고 commit·push를 시도해
   **탐지되는지** 확인한다. v0.1은 64 KiB 이후를 보지 못했다.
6. 10 MiB를 넘는 텍스트 파일을 docs에 두고 push한다. 조용히 통과하지 않고
   "너무 커서 스캔할 수 없다"는 오류로 게이트가 fail-closed인지 확인한다.
7. 양쪽에서 symlink를 서로 다른 대상으로 바꿔 `sanho sync` 충돌을 만들고, 해소
   후 결과가 정상 symlink인지 확인한다.

어느 단계에서든 파일이 조용히 사라지거나 일반 파일로 바뀌면 FAIL이다.

## H12. 설치 binary와 PATH 경계 (변형)

내부 함수를 직접 호출하지 않고 공개 설치 binary와 실제 git 명령만 사용한다.

1. 임시 `GOBIN`에 검증할 release를 설치하고 `command -v sanho`,
   `sanho version --json`, `go version -m`을 기록한다. checkout에서 빌드한
   binary와 섞지 않는다.
2. 그 binary로 새 작업공간을 init하고 정상 흐름을 한 바퀴 돈다.
3. 설치된 hook이 release binary의 canonical 절대 경로를 shell 인용해 기록했는지
   확인한다. GUI Git 클라이언트에서 PATH를 비운 채 commit과 push를 실행해도
   같은 binary가 실행되어야 한다. 그 binary를 임시로 치우면 commit/post hook은
   fail-open, `pre-push`는 fail-closed인지 확인한 뒤 즉시 복원한다.
4. `SANHO_HOME`을 절대 경로로 지정해 격리된 레지스트리로 전체 흐름을 다시
   돌린다. 상대 경로를 주면 거절하는지도 확인한다.
   ```text
   sanho: SANHO_HOME must be an absolute path
   ```
5. `~/.sanho` 디렉터리 권한이 `0700`, `state.json`과 `.bak`이 `0600`인지
   확인한다. 일부러 `0755`로 바꾼 뒤 다음 실행에서 다시 조여지는지 확인한다.
6. 두 Sanho 프로세스를 동시에 실행해 레지스트리 잠금이 동작하는지 확인한다.
   한쪽이 5초 안에 다음 메시지로 물러나는지 본다.
   ```text
   another sanho process holds <lock-path>; retry in a moment
   ```
7. `~/.sanho/state.json`을 일부러 손상시키고 `.bak`에서 자동 복구되는지,
   둘 다 손상시키면 **빈 상태로 시작하지 않고** 두 경로를 이름지어 실패하는지
   확인한다.
8. 실행 권한이 없는 hook 파일을 만들어 두고 `sanho init`이 owner execute만
   추가해 고치는지 확인한다.
9. 모든 hook의 실행 비트를 제거한 뒤 docs commit과 app push를 완료한다.
   canonical이 움직이지 않은 상태에서 `sanho status` text와 JSON이 각각
   publication pending을 보고하는지 확인한다.
10. `sanho doctor --fix`가 hook을 복구하고 publication 경고를 출력하는지
    확인한다. 같은 tip의 no-op `git push`만으로는 canonical이 움직이지 않아야
    하며, 새 docs-changing commit 뒤 push하면 수렴해야 한다.

## 릴리스 판정

다음 조건을 모두 만족해야 hands-on 관점에서 릴리스 가능으로 판정한다.

- 필수 시나리오가 PASS이거나, 실행할 수 없었던 항목이 BLOCKED 사유와 잔여
  위험으로 명시돼 있다.
- 시작·종료 canonical head SHA와 각 작업공간의 `.sanho_base.json`이 설명
  가능하고, 의도하지 않은 ref가 없다.
- 모든 작업공간에서 `git status --porcelain=v1`, `sanho status --json`,
  `sanho doctor --json`을 기록했으며 남은 `sync_in_progress` 상태가 없다.
- canonical 이력이 선형이고 merge commit이 없으며, force push가 사용되지
  않았다.
- H05의 측정치가 기록돼 있고, 임계를 넘는 항목은 잔여 위험으로 명시돼 있다.
- 임시 branch, clone, `GOBIN`, `SANHO_HOME`, 자격 증명 설정을 정리했다.
- 유지한 validation 파일과 commit은 소유 저장소와 유지 이유가 기록돼 있다.
- 전체 release diff와 자동·hands-on 증적을 사용자에게 먼저 제출하고, 사용자가
  해당 결과를 검토한 뒤 별도의 명시적 최종 릴리스 승인을 제공했다. 구현·검증
  지시나 과거의 일반적인 릴리스 요청은 이 최종 승인으로 간주하지 않는다.
