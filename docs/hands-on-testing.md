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
5. 첫 push가 같은 디렉터리의 임시 파일과 atomic rename으로 hook을 갱신하고
   중단되는지 확인한다. 실행 중 shell에 `origin: command not found` 또는 URL 실행
   오류가 없어야 하고 기존 custom 내용과 mode가 같아야 한다. 두 번째 push가 정상
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
6. active operation에서 lifecycle hook은 간결한 defer 안내 후 성공하되 commit
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
9. backend 없이 standalone `REBASE_HEAD`만 남긴 복사본에서 normal docs commit이
   실패하고 exact path/OID와 conditional `update-ref -d`만 안내하는지 확인한다.
   malformed marker에는 삭제 명령이 없어야 한다. 지원 절차로 marker를 제거한 뒤
   Sanho 명령이 정상 동작하는지 확인한다.
10. 사용자 의도에 맞게 continue, abort 또는 quit한 뒤 backend와 unmerged index,
    orphan marker를 각각 확인한다. metadata가 clear된 경우 다음 pre-commit 또는
    pre-push가 valid HEAD를 수렴시키며 기존 pull-commit recovery와 main publication이
    정상 동작하는지 확인한다.

`--quit`과 `--abort`의 결과를 같은 것으로 취급하거나 operation metadata를
직접 삭제해 성공시키면 이 시나리오는 FAIL이다.

## H12. rebase lifecycle 무변경과 종료 후 수렴

이 시나리오는 실제 rebase 중 Sanho state가 변하지 않고, 종료 후에는
`post-rewrite` 유무와 무관하게 valid HEAD로 수렴하는지 검사한다.

1. 기본 merge backend용 application clone에 docs-version이 다른 `main`과
   unpublished feature branch를 만든다. feature에는 실제 파일 변경 commit
   하나와 의도적으로 빈 commit을 포함해 최소 1,000개 commit을 만들고
   prepared transaction의 HEAD, index tree, docs hash와 workspace report
   checksum을 기록한다.
2. `git --version`, `git rev-parse --show-object-format`, 시작 시각과
   `git rev-parse --git-path rebase-merge` 결과를 기록하고 기본 merge backend의
   `git rebase main`을 실행한다.
3. 종료 시간과 hook 출력을 기록한다. active backend 중에는 간결한 defer 안내만
   있어야 하며 recovery 명령 전체를 출력하지 않는다. transaction, rewrite 기록,
   docs hash, workspace report, refs, index와 worktree checksum은 Git 자체 변경을
   제외하고 같아야 한다.
4. metadata가 clear된 뒤 `sanho status --json`이 valid HEAD의
   `head_reconciliation.pending: true`를 표시하면서 canonical HEAD와 같으면
   최상위 `status: up_to_date`인지 확인한다. status 전후 checksum은 같아야 한다.
5. 정상 pre-commit과 pre-push를 각각 사용해 `.sanho_docs_hash`와 daemon workspace
   hash가 새 docs-version으로 멱등 수렴하는지 확인한다.
6. apply backend용 별도 복사본에는 빈 commit fixture를 재사용하지 않는다.
   전용 counter 파일을 매 commit마다 변경하는 방식으로 최소 1,000개의
   non-empty commit을 만든 뒤 `git rebase --apply main`을 실행하고 active
   backend가 `rebase-apply`였음을 기록한다. `Patch is empty`로
   rebase가 Sanho hook 전에 중단되면 제품 실패가 아니라 fixture 오류이므로
   non-empty commit으로 다시 구성한다. 유효한 apply rebase는 merge backend와
   동일한 metadata 및 Git 상태 조건을 만족해야 한다.
7. fast-forward, up-to-date/no-op, commit rewrite, conflict 후 continue, abort,
   quit를 별도 복사본에서 실행한다. abort는 원래 HEAD/hash를 유지하고, quit 뒤
   unmerged index 또는 orphan marker가 남으면 pre-push가 계속 차단돼야 한다.

active backend 중 일부 Sanho metadata라도 갱신하거나, fast-forward/no-op를
`post-rewrite`가 없다는 이유로 제외하거나, operation metadata를 수동 삭제해
성공시키면 이 시나리오는 FAIL이다.

## H13. pre-push docs provenance 무결성

이 시나리오는 incident를 실제 설치 hook과 실제 push로 재현하고 모든 제안 branch
OID가 remote 변경 전에 검증되는지 확인한다.

1. 모든 Sanho hook을 설치하고 valid canonical docs/application commit을 만든다.
2. standalone `REBASE_HEAD`만 남긴 뒤 normal docs commit이 provenance 없이
   성공하지 못하고 HEAD, index, worktree, marker, transaction과 remote ref가
   보존되는지 확인한다. 지원 절차로 marker를 조건부 제거한다.
3. fixture 전용 빈 hooksPath로 docs 변경과 trailer 없는 unmanaged commit을 만든다.
   이 우회는 제품 복구 절차가 아니라 negative fixture 구성에만 사용한다.
4. valid branch와 unmanaged branch를 한 실제 push로 제안한다. pre-push가 stdin의
   두 local OID를 검사해 전체 push를 거부하고 어느 remote ref도 바꾸지 않아야 한다.
5. 별도 복사본에서 full OID 형식의 unknown/forged trailer, 중복 trailer, canonical
   snapshot과 다른 tree를 각각 제안해 같은 fail-closed 결과를 확인한다.
6. 정상 hook으로 docs-changing commit을 만들어 canonical 게시와 trailer를 확인한
   뒤 push한다. 그 commit의 non-doc 후손과 서로 다른 valid OID의 multi-ref push도
   성공해야 한다.
7. exact 두 줄 legacy pre-push hook을 설치해 실제 origin push와 직접 URL push를
   실행한다. 첫 시도는 atomic upgrade와 재시도를 안내하되 `origin` 또는 URL을 shell
   command로 실행하지 않고, 안내된 재시도 뒤 local/remote tip과 status가 일치해야 한다.
8. 이미 게시된 invalid branch 복사본은 [지원 복구 절차](recovery.md)를 실행해
   staged/unstaged 변경, complete tree와 lease를 검증하고 `status: up_to_date`로 끝낸다.

direct `sanho hook pre-push` 호출만으로 remote 무변경을 증명하거나, negative fixture
외부에서 hook을 우회하거나, unrestricted force push를 사용하면 FAIL이다.

## H14. 설치 hook trust boundary와 status 호환성

이 시나리오는 내부 installer나 hook 함수를 직접 호출하지 않고 공개 설치 binary와
실제 Git 명령만으로 hook 설치 경계를 확인한다.

1. 임시 `GOBIN`에 검증할 release의 `sanho`와 `sanhod`를 설치하고
   `command -v`, `sanho version`, `go version -m`을 기록한다. checkout에서 빌드한
   binary와 섞지 않는다.
2. application clone의 `pre-commit`에 custom 명령과 `sanho hook pre-commit`을
   포함한 mode `0644` 파일을 둔 뒤 `sanho init`을 실행한다. custom 내용과 기존
   permission bit는 유지되고 owner execute가 추가돼야 하며, 실제 `git commit`에서
   custom marker와 Sanho 검사가 모두 실행돼야 한다.
3. 같은 repository의 linked worktree에서 별도 `sanho init`을 실행한다. hook은
   `git rev-parse --git-path hooks`가 가리키는 common directory에 있어야 하고
   worktree private gitdir의 `hooks`에는 생성되지 않아야 한다. linked worktree에만
   standalone `REBASE_HEAD`를 둔 실제 commit은 차단되고 다른 worktree 상태는
   바뀌지 않아야 한다.
4. disposable clone에 `rebase-merge`와 `rebase-apply`를 함께 만들어 operation
   inspection error를 유도한다. 실제 pre-commit은 commit을 차단하고 HEAD, index,
   worktree, metadata와 remote refs를 보존해야 한다. lifecycle hook은 state를
   변경하지 않고 성공해야 한다.
5. fast-forward/no-op rebase 뒤 local docs hash만 이전 값인 상태에서
   `sanho status --json`을 실행한다. 기존 `docs_relation`은 local hash 기준
   `behind`를 유지하면서 최상위 `status`는 `up_to_date`,
   `head_reconciliation.pending`은 `true`여야 한다.
6. merge와 apply backend rebase를 모든 설치 hook과 함께 실행한다. active backend
   중에는 Sanho state를 변경하지 않는다. backend clear 뒤 Git이 lifecycle hook을
   제공하면 즉시 reconciled, 제공하지 않으면 pending이어야 하며 다음 pre-commit
   또는 pre-push에서 멱등하게 수렴해야 한다.
7. exact 두 줄 legacy pre-push hook으로 direct URL push를 실행한다. 첫 시도는
   atomic upgrade만 수행하고, 두 번째 direct URL 시도는 pending `origin/main`
   선행 게시를 안내해야 한다. `git push origin main` 성공 뒤 같은 direct URL
   push가 성공하고 local/remote tip과 status가 일치해야 한다.

비실행 hook이 경고만 남기고 무시되거나, linked worktree private hook에 의존하거나,
operation 검사 실패 중 commit이 성공하거나, 기존 JSON field 의미를 바꾸면 FAIL이다.

## 릴리스 판정

다음 조건을 모두 만족해야 hands-on 관점에서 릴리스 가능으로 판정한다.

- 필수 시나리오가 PASS이거나, 실행할 수 없었던 항목이 BLOCKED 사유와 잔여
  위험으로 명시돼 있다.
- 시작·종료 remote SHA와 docs hash가 설명 가능하고 의도하지 않은 ref가 없다.
- 모든 workspace에서 `git status --porcelain=v1`과 `sanho status --json`을
  기록했으며 남은 pending·ambiguous·corrupt 상태가 없다.
- 임시 branch, alias remote, clone, daemon과 service 변경을 정리했다.
- 유지한 validation 파일과 commit은 소유 저장소와 유지 이유가 기록돼 있다.
- 전체 release diff와 자동·hands-on 증적을 사용자에게 먼저 제출하고, 사용자가
  해당 결과를 검토한 뒤 별도의 명시적 최종 릴리스 승인을 제공했다. 구현·검증
  지시나 과거의 일반적인 릴리스 요청은 이 최종 승인으로 간주하지 않는다.
