# Sanho hands-on 테스트

## 목적과 원칙

이 문서는 실제 Git remote, 인증, branch rule, service manager처럼 자동 E2E가
완전히 재현하기 어려운 경계를 릴리스 전에 빠뜨리지 않고 확인하기 위한
체크리스트다. 각 항목은 자동 테스트를 대체하지 않으며 `make test`가 통과한
release candidate로만 수행한다.

- 실제 저장소를 사용할 때는 대상과 허용된 write 범위를 먼저 기록한다.
- force push, hook 우회, `.git/sanho` 수동 삭제는 사용하지 않는다.
- 검증용 branch와 파일 이름에는 날짜와 실행 ID를 넣어 기존 작업과 충돌하지
  않게 한다.
- 실패도 유효한 증거다. branch rule이나 network 실패를 우회하지 말고
  출력과 전후 SHA를 기록한다.
- Sanho 저장소의 push, tag, release, install은 별도 승인이 있을 때만 한다.

## 공통 실행 기록

테스트 실행마다 아래 양식을 복사해 채운다.

```text
실행 ID:
실행 시각 / 테스트 담당자:
OS / Git / Go 버전:
sanho version --json:
sanhod --version:
sanho / sanhod 절대 경로:
SANHO_HOME / SANHO_SOCKET 또는 service 설정:
대상 저장소와 허용된 write 범위:
시작 remote main SHA:
시작 local HEAD / branch / git status --porcelain=v1:
시작 sanho status --json:
예상 결과:
실제 명령과 종료 코드:
실제 결과와 주요 출력:
종료 remote main SHA:
종료 docs hash / sanho status --json:
정리한 branch, clone, daemon, 임시 경로:
남겨 둔 검증 파일과 이유:
판정: PASS / FAIL / BLOCKED
```

공통 사전 점검은 다음과 같다.

```bash
command -v sanho
command -v sanhod
sanho version --json
git status --porcelain=v1
git remote -v
git ls-remote origin refs/heads/main
sanho status --json
```

실행 중에는 중요한 단계마다 local HEAD, `origin/main`, 실제 remote main,
`sanho status --json`을 다시 기록한다. 테스트가 끝나면 임시 clone과 독립
daemon을 종료하고 검증 branch를 삭제하되, 유지하기로 한 validation 파일과
그 commit SHA는 실행 기록에 남긴다.

## H01. 실제 세 저장소 양방향 동기화와 main 게시

대상은 실제 `sanho-server`, `sanho-client`, `sanho-docs`다. 로컬 bare remote로는
인증, Git hosting과 세 저장소 사이의 전파를 증명할 수 없어 hands-on으로
수행한다.

1. 세 remote main SHA와 clean status를 기록하고 새 Sanho binary, 격리
   daemon과 격리 clone을 사용한다.
2. docs 저장소에 실행 ID가 포함된 validation 문서를 fast-forward commit으로
   게시한다.
3. server와 client에서 `sanho pull` 또는 `sanho pull-commit`으로 같은 docs
   hash를 받는지 확인한다.
4. 한 application 저장소에는 staged non-doc, unstaged non-doc, untracked 파일을
   각각 둔 상태로 `pull-commit`을 실행하고 세 layer가 보존되는지 확인한다.
5. docs 변경을 application history에 반영한 뒤 feature branch를
   `git push origin <branch>`로 게시한다. `origin/main`이 먼저 fast-forward되고
   target branch가 뒤이어 게시되는지 확인한다.
6. application docs의 정상 변경을 canonical docs에 게시하고 다른 application
   저장소가 그 변경을 다시 받는지 확인한다.
7. 세 workspace의 최종 docs hash, `up_to_date` 상태와 remote SHA를 기록한다.

검증 branch는 삭제한다. validation 파일을 유지하기로 했다면 세 저장소에서
의도한 최종 위치와 해당 commit SHA를 명시한다.

## H02. direct URL과 alias remote 우회 차단

Git이 pre-push hook에 전달하는 첫 번째 인자는 remote 이름일 수도 있고 URL일
수도 있어 실제 `git push`로 확인한다.

1. application 저장소에 main 게시 대기 상태와 아직 remote에 없는 검증
   branch를 만든다.
2. `git remote add <test-alias> <origin-url>`을 추가한다.
3. `git push <test-alias> <branch>`와 `git push <origin-url> <branch>`를 각각
   실행한다.
4. 두 push가 `git push origin main`을 먼저 실행하라는 메시지와 함께 실패하고,
   main과 target remote SHA가 모두 바뀌지 않았는지 확인한다.
5. `git push origin main`으로 pending 상태를 해소한 뒤 같은 alias/URL push가
   허용되는지 확인한다.
6. pending 상태에서 tag-only push와 branch deletion은 영향을 받지 않는지도
   별도로 확인한다.

## H03. 실제 branch rule에 의한 main 거부

hosting service의 보호 규칙과 사용자 권한은 로컬 bare remote가 대신할 수
없다.

1. 직접 push가 금지된 test repository 또는 승인된 보호 시간대를 사용한다.
2. main 게시 대기 상태에서 origin의 feature branch를 push한다.
3. 선행 main push가 branch rule에 의해 거부되고 target branch도 생성·갱신되지
   않는지 확인한다.
4. publication state의 `classification`, `reason`, `last_error`가 남고 force
   push나 우회가 시도되지 않았는지 확인한다.
5. 규칙 또는 승인 절차를 정상적으로 충족한 뒤 같은 push를 재시도한다.

## H04. main 성공 후 target push 실패와 재시도

1. main은 허용하고 검증 target branch만 거부하는 임시 server-side rule을
   승인된 test repository에 설정한다.
2. pending 상태에서 target branch를 push한다.
3. main은 remote에 도달하고 publication state가 정리됐지만 target은 거부됐는지
   확인한다.
4. 거부 조건을 해소하고 같은 target push를 재시도한다.
5. 두 번째 시도에서 main을 중복 게시하지 않고 target만 게시하는지 확인한다.

## H05. remote main 경합과 divergence

1. 두 개의 격리 clone에서 같은 origin/main을 기준으로 시작한다.
2. 첫 clone에 Sanho system commit과 publication state를 만들고, 둘째 clone에서
   승인된 별도 main commit을 먼저 fast-forward push한다.
3. 첫 clone의 target push가 remote main 변화를 감지해 실패하고 target ref를
   만들지 않는지 확인한다.
4. `sanho status --json`의 blocked 이유와 모든 local/remote SHA를 기록한다.
5. 정상 fetch와 명시적인 history 정리 후 fast-forward 가능한 상태에서 다시
   시도한다. force push는 사용하지 않는다.

## H06. v0.1.2 legacy v2 중단 복구

이 항목은 과거 release가 만든 실제 transaction artifact와 새 binary 사이의
호환성을 확인한다.

1. disposable repository와 격리 daemon에서 v0.1.2 CLI·hook으로 version 2
   prepared transaction을 만든 뒤 post-rewrite 완료 전에 프로세스를
   중단한다. state와 여섯 snapshot artifact를 복사해 보존한다.
2. 정상 case에서는 docs를 바꾸지 않는 non-doc amend를 만든다. 새 binary의
   `sanho status --json`과 `sanho pull-commit --recover`가
   `recoverable_rewrite`로 판정하고 HEAD, index, worktree를 보존하는지 확인한다.
3. docs amend case에서는 준비된 merged index와 동일한 docs amend가 복구되는지
   확인한다.
4. tampered case는 같은 초기 artifact의 새 복사본에서 HEAD docs만 다르게
   amend한다. `ambiguous`로 남고 push가 차단되는지 확인한다.
5. snapshot 누락 및 손상 복사본은 각각 `corrupt`가 되며 transaction과
   `refs/sanho/recovery/<transaction-id>/`가 유지되는지 확인한다.
6. 동일 recovery 명령을 반복해도 결과와 Git 상태가 멱등적인지 확인한다.

private state를 손으로 고쳐 성공 상태를 만들지 않는다. 테스트용 복사본의
artifact 삭제·손상은 실패 시나리오를 만드는 단계에서만 수행한다.

## H07. linked worktree queue와 동시 실행

1. 하나의 application repository에 `git worktree add`로 두 linked worktree를
   만든다.
2. 첫 worktree에서 system commit을 만들어 main publication을 pending으로
   둔다.
3. 둘째 worktree의 `sanho status --json`도 같은 Git common directory의 queue를
   보는지 확인한다.
4. 두 worktree에서 거의 동시에 origin branch push를 실행한다.
5. main이 한 번만 fast-forward되고, nested push나 state 손상 없이 두 결과가
   설명 가능한 순서로 끝나는지 확인한다.
6. worktree별 staged/unstaged 파일이 서로 섞이지 않았는지 확인한다.

## H08. custom hooksPath와 legacy hook 갱신

1. repository-local `core.hooksPath`와 사용자 전역 hooksPath를 각각 사용하는
   disposable clone을 준비한다.
2. 기존 custom hook의 내용과 mode를 기록한 뒤 `sanho init`을 실행한다.
3. Sanho hook과 기존 hook이 계약대로 공존하고 remote 인자 `"$@"`가 전달되는지
   실제 push로 확인한다.
4. remote 인자를 전달하지 않는 v0.1.2 pre-push hook으로 교체해 pending push를
   실행한다.
5. 첫 push가 hook을 제자리에서 갱신하고 중단되며, 두 번째 push가 정상
   publication 흐름을 수행하는지 확인한다.
6. `sanho clean` 후 Sanho가 소유한 hook 부분만 제거되는지 확인한다.

## H09. SSH·network 실패와 재시도

1. 격리 환경에서 잘못된 SSH key, 끊긴 network, DNS 실패 중 승인된 방법으로
   fetch와 push 실패를 각각 만든다.
2. `sanho status`, docs pull/push, application pre-push가 stale 성공으로
   처리되지 않고 원인을 표시하는지 확인한다.
3. main 선행 push 실패 시 target ref와 publication state가 보존되는지 확인한다.
4. 인증 또는 network를 복구한 뒤 동일 명령을 재시도해 별도 state 삭제 없이
   성공하는지 확인한다.
5. credential, SSH agent와 proxy 설정을 원래 상태로 복구한다.

## H10. launchd/systemd 재시작과 업그레이드

1. 실제 사용자 service 설정, binary 절대 경로, state와 socket 경로를
   기록하고 health 및 `sanho state --all` 기준선을 남긴다.
2. 진행 중 요청이 없는 상태에서 service manager로 정상 종료·재시작하고 stale
   socket 없이 기존 project/workspace를 읽는지 확인한다.
3. 지원되는 이전 release에서 release candidate로 CLI와 daemon을 함께
   교체한다. 서로 다른 버전을 섞지 않는다.
4. health, state, 기존 workspace status와 실제 pull/push smoke를 확인한다.
5. 실패 시 보존한 state와 두 binary를 사용해 문서화된 rollback을 수행하고
   다시 health를 확인한다.

service 등록 명령과 책임 경계는 [배포 규칙](deployment.md)을 따른다.

## H11. clean stale Git operation 차단과 linked worktree 격리

이 시나리오는 ref와 worktree가 정상이더라도 operation metadata만 남은 실제
장애를 재현한다. 반드시 폐기 가능한 repository에서 수행한다.

1. bare origin과 application clone을 만들고 두 개 이상의 commit을 main에
   push한다. 시작 `HEAD`, `origin/main`, index tree와 worktree checksum을
   기록한다.
2. interactive rebase를 중간 `exec false` 또는 승인된 중단점에서 멈춘 뒤
   `git reset --hard refs/heads/main`으로 HEAD와 worktree만 main에 맞춘다.
   이 reset은 rebase metadata를 제거하지 않는다는 점을 확인한다.
3. `HEAD == origin/main`, 빈 `git status --porcelain`과 동시에 `git status`가
   rebase 진행 중임을 보고하는지 확인한다.
4. `sanho status`와 `sanho status --json`을 두 번씩 실행한다. 두 결과의
   `git_operation`이 동일하고 application workspace의 refs, index, worktree,
   rebase metadata와 workspace-local pull-commit/main-publication state
   checksum이 바뀌지 않았는지 확인한다. daemon의 managed docs clone은 project
   status 조회 중 canonical remote를 기준으로 refresh될 수 있으므로 이
   checksum 대상에 포함하지 않는다.
5. `init`, `pull`, `pull-commit`의 모든 mode, `fix`, 실제 `clean`과 pre-push가
   operation 및 복구 후보를 설명하며 실패하는지 확인한다. 각 명령 전후의
   local/remote refs와 checksum이 같아야 한다. `clean --dry-run`은 같은
   상태에서 성공하되 application workspace를 변경하지 않아야 한다.
6. paused/stale operation에서 lifecycle hook은 경고 후 성공하되 commit
   message, transaction과 daemon state를 바꾸지 않고, pre-push만 계속
   실패하는지 확인한다. empty·malformed·HEAD에서 도달할 수 없는 mapping뿐
   아니라, 유효하고 도달 가능한 full OID mapping을 pipe나 다른 regular
   file로 넣은 수동 `post-rewrite rebase`도 같은 결과여야 한다.
7. 별도 복사본에서 conflict가 있는 rebase, merge, cherry-pick, revert와
   bisect를 만들어 정확한 type이 보고되는지 확인한다.
8. `git worktree add`로 linked worktree를 만들고 한 worktree에만 operation을
   시작한다. 해당 worktree의 Sanho mutation만 차단되고 다른 worktree는
   operation이 없는 것으로 보고되는지 확인한다. linked worktree의 `.git`이
   파일인 상태도 함께 기록한다.
9. 사용자 의도에 맞게 continue, abort 또는 quit한 뒤 Sanho 명령을 다시
   실행해 기존 pull-commit recovery와 main publication이 정상 동작하는지
   확인한다.
10. 별도 복사본에서 docs-version이 다른 main 위로 실제 rebase를 성공시킨다.
    Git이 제공한 mapping으로 prepared head와 rewrite 기록, docs hash와 daemon
    workspace hash가 새 HEAD에 맞춰지고 refs, index, worktree는 hook 때문에
    추가로 바뀌지 않는지 확인한다. daemon이 없으면 같은 docs hash의 pending
    workspace report가 남아야 한다.

`--quit`과 `--abort`의 결과를 같은 것으로 취급하거나 operation metadata를
직접 삭제해 성공시키면 이 시나리오는 FAIL이다.

## H12. 대규모 post-rewrite reconciliation

이 시나리오는 대규모 실제 rebase가 hook 제한 시간 때문에 Sanho metadata만
남기는 회귀를 검사한다. daemon 연결·미연결 경우를 분리된 폐기 가능한
복사본에서 수행한다.

1. application clone에 docs-version이 다른 `main`과 unpublished feature
   branch를 만든다. feature에는 실제 파일 변경 commit 하나와 빈 commit을
   포함해 최소 1,000개 commit을 만들고 prepared transaction의 HEAD, index
   tree, docs hash와 workspace report checksum을 기록한다.
2. `git --version`, `git rev-parse --show-object-format`, 시작 시각과
   `git rev-parse --git-path rebase-merge/rewritten-list` 결과를 기록한 뒤
   hook에서 호출하는 `git`만 감싸는 wrapper를 준비한다. wrapper는 인자를
   trace 파일에 기록하고 `rev-parse --verify *^{tree}` 호출에만 40ms 지연을
   추가한 뒤 실제 Git을 실행한다. 이 wrapper를 hook의 `PATH` 앞에 둔 상태로
   기본 merge backend의 `git rebase main`을 실행한다.
3. 종료 시간과 hook 출력을 기록한다. `signal: killed`,
   `context deadline exceeded`, `Sanho mutation was skipped`가 없어야 하며
   rewrite 수가 실제 mapping 수와 일치해야 한다. trace에는
   `cat-file --batch-check`와 `rev-list --no-walk=unsorted --stdin`이 각각
   한 번만 있어야 하고 `rev-parse --verify *^{tree}`는 없어야 한다.
4. prepared head가 mapping의 새 HEAD로 바뀌고 docs hash와 daemon workspace
   hash가 새 docs-version과 일치하는지 확인한다. hook이 rebase 결과 외에
   refs, index와 worktree를 추가로 바꾸지 않았는지 checksum으로 확인한다.
5. daemon을 중지한 복사본에서 같은 검사를 반복한다. rebase는 성공하고 같은
   docs hash의 pending workspace report가 남아야 한다.
6. 별도 복사본에서 `git rebase --apply main`을 실행하고 active backend가
   `rebase-apply/rewritten`이었음을 기록한다. merge backend와 동일한 metadata
   및 Git 상태 조건을 만족해야 한다.
7. 각 복사본에서 `sanho status --json`을 기록한다. 정상 reconciliation 뒤
   pending·ambiguous·corrupt transaction이 없어야 하며 임시 clone과 daemon을
   정리한다.

mapping 수나 지연을 줄여 통과시키거나, wrapper를 application의 일반 Git
명령 전체에 적용해 결과를 오염시키거나, timeout 뒤 transaction 또는 rebase
metadata를 수동 삭제하면 이 시나리오는 FAIL이다.

## H13. post-rewrite 형식 전방 호환성과 위조 차단

이 시나리오는 Git이 old/new OID 뒤에 optional extra-info를 추가하는 경우와
내용만 복제한 위조 input을 구분하는지 확인한다. 실제 복구 절차가 아니라
폐기 가능한 fixture에서만 active backend 파일을 조작한다.

1. prepared transaction이 있는 feature branch에서 여러 commit의 rebase를
   `--exec false`로 중단한다. `git status`로 paused rebase를 확인하고 active
   backend의 `rewritten-list` 또는 `rewritten` 절대 경로를
   `git rev-parse --git-path`로 구한다.
2. 원본 rewrite 파일, HEAD, refs, index tree, worktree, transaction, docs hash와
   workspace report checksum을 기록한다. 원본 파일은 fixture 밖에 증거용으로
   복사하되 positive 입력에는 복사본을 사용하지 않는다.
3. active backend 파일의 각 non-empty line에 `future metadata remains opaque`
   를 덧붙인다. `sanho hook post-rewrite rebase < "$rewrite_file"`을 실행해
   evidence validation 경고 없이 첫 두 OID의 mapping만 기록되는지 확인한다.
   extra-info 자체는 transaction이나 JSON에 저장되면 안 된다.
4. hook 전후 refs, index와 worktree checksum이 같고, 검증된 mapping에 해당하는
   Sanho transaction·docs hash·workspace report만 바뀌었는지 확인한다.
5. 같은 내용을 `printf` pipe와 복사한 regular file에서 각각 전달한다. 두
   경우 모두 provenance 경고 후 Sanho metadata가 바뀌지 않아야 하며 hook은
   Git을 막지 않도록 종료 코드 0을 반환해야 한다.
6. active backend 파일을 매번 원본에서 복원한 뒤 한 필드 line, 짧거나 대문자인
   OID, 존재하지 않는 OID, blob OID, HEAD에서 도달 불가능한 commit OID를
   차례로 넣는다. 각 경우 경고, 종료 코드 0과 모든 Sanho/Git checksum 불변을
   확인한다.
7. fixture의 원본 파일을 복원하고 사용자 의도에 따라 rebase를 abort 또는
   continue한다. 종료 후 `sanho status --json`을 기록하고 임시 저장소를
   삭제한다.

extra-info를 permit 근거로 사용하거나 pipe·복사 파일을 허용하거나 malformed
첫 두 필드에서 일부 metadata를 갱신하면 이 시나리오는 FAIL이다.

## 릴리스 판정

다음 조건을 모두 만족해야 hands-on 관점에서 릴리스 가능으로 판정한다.

- 필수 시나리오가 PASS이거나, 실행할 수 없었던 항목이 BLOCKED 사유와 잔여
  위험으로 명시돼 있다.
- 시작·종료 remote SHA와 docs hash가 설명 가능하고 의도하지 않은 ref가 없다.
- 모든 workspace에서 `git status --porcelain=v1`과 `sanho status --json`을
  기록했으며 남은 pending·ambiguous·corrupt 상태가 없다.
- 임시 branch, alias remote, clone, daemon과 service 변경을 정리했다.
- 유지한 validation 파일과 commit은 소유 저장소와 유지 이유가 기록돼 있다.
