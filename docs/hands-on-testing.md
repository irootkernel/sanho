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
SSH 인증·network 차단, 진짜 v0.1 설치본에서 출발하는 migration, 운영체제의
filesystem semantics, 설치 binary와 GUI Git 환경이다.

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

## 자동 테스트로 이전한 경계

다음 항목은 반복 가능하고 판정이 명확하므로 hands-on ID를 부여하지 않는다.

| 경계 | 자동 검증 |
|---|---|
| 서로 다른 머신을 모사한 canonical CAS 경합 | `test/cli/e2e`가 서로 다른 `SANHO_HOME`을 쓰는 두 실제 프로세스의 다른 파일·같은 줄 동시 push를 실행한다. 선형 이력, 명시적 충돌 해소, 머신 간 sibling 비가시성을 확인한다. |
| canonical 서버 측 거부와 게시 branch 선택 | `test/cli/integration`이 bare remote의 `pre-receive` 거부 전후 ref 불변성과 같은 push의 재시도를 검증한다. `internal/infra/canonical`은 `main`이 없고 `master`만 있는 origin 선택을 검증한다. |
| 대형 docs correctness와 측정 | `SANHO_SCALE=1 make test-scale`이 1,000 files, 500 commits, 약 50 MiB fixture에서 init·status·push·sync 시간을 기록한다. 선택적 profile이며 `make test`와 Gaori `all`에는 포함되지 않는다. |

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

## H02. 오프라인 경계 — commit은 되고 push는 안 된다 (신설)

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

## H03. 실사용 v0.1 → v0.2 migration (신설)

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
   원본이 그대로 남아 있는지 확인한다. 두 hash 파일이 모두
   `git check-ignore`에 잡히고 일반 `git status`에는 나타나지 않아야 한다.
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

### H03을 production과 격리해서 실행하는 방법

실제 v0.1 binary가 남아 있다면 production daemon과 `~/.sanho`를 migration하지
않고도 위 절차를 반복할 수 있다. 새 로컬 bare canonical과 두 개 이상의 새
application 저장소를 준비하고, 모든 상태를 실행별 임시 디렉터리에 둔다.

```bash
h03_root=$(mktemp -d /tmp/sanho-h03.XXXXXX)
h03_home="$h03_root/v1-home"
h03_socket="$h03_root/sanhod.sock"
mkdir -p "$h03_home"
chmod 0700 "$h03_home"

# 별도 terminal에서 실행한다. production service를 unload하지 않는다.
/path/to/v0.1.6/sanhod -home "$h03_home" -socket "$h03_socket"

# v0.1 workspace는 반드시 이 socket을 명시해서 만든다.
/path/to/v0.1.6/sanho --socket "$h03_socket" init \
  --project <fixture-project> \
  --docs-repo-url "$h03_root/canonical.git" \
  --docs-dir docs
```

application fixture에도 `origin`이 있어야 한다. v0.1은 workspace 등록 요청에
현재 저장소의 origin URL을 넣으므로 origin이 없으면 `missing required fields`로
초기화가 거절된다.

binary 교체 상태는 설치 경로를 덮어쓰지 않고 임시 `PATH`로 만든다. hook은
`sanho`라는 이름을 호출하므로 Git을 실행한 process의 `PATH`가 곧 교체 경계다.

```bash
mkdir -p "$h03_root/v2-bin"
ln -s /absolute/path/to/checkout/bin/sanho "$h03_root/v2-bin/sanho"

SANHO_HOME="$h03_home" \
PATH="$h03_root/v2-bin:$PATH" git commit -m 'docs: migration fixture'
SANHO_HOME="$h03_home" \
PATH="$h03_root/v2-bin:$PATH" git push
SANHO_HOME="$h03_home" \
  /absolute/path/to/checkout/bin/sanho migrate
```

transaction 거절은 managed 파일을 직접 만들지 말고, base와 canonical 양쪽에서
같은 파일을 다르게 수정한 뒤 v0.1 `sanho pull-commit`으로 진짜 conflict
transaction을 만든다. v0.2의 거절을 확인한 뒤 같은 v0.1 binary와 socket으로
`sanho pull-commit --abort`를 실행해 원래 staged/unstaged 상태가 복원되는지
확인한다.

migrate 직전과 직후에는 bare canonical의 refs와 tree를 비교한다. 정상적인
commit → sync → push 확인으로 canonical을 전진시키기 **전**에 비교해야 migration
자체의 불변성을 증명할 수 있다.

```bash
git --git-dir="$h03_root/canonical.git" show-ref | sort >before.refs
git --git-dir="$h03_root/canonical.git" rev-parse 'main^{tree}' >before.tree
# sanho migrate
git --git-dir="$h03_root/canonical.git" show-ref | sort >after.refs
git --git-dir="$h03_root/canonical.git" rev-parse 'main^{tree}' >after.tree
diff -u before.refs after.refs
diff -u before.tree after.tree
```

service lifecycle도 확인하려면 production과 다른 label(예:
`xyz.rootkernel.sanho.h03.<실행-ID>`)과 임시 plist를 사용한다. plist의 `-home`,
`-socket`, stdout, stderr가 모두 위 임시 디렉터리를 가리키는지 확인한 뒤에만
bootstrap/bootout한다. migrate가 출력한 고정 production label의 bootout 명령은
이 격리 시험에서 실행하지 않는다. 시작 전후에 production label의 PID와 plist,
`~/.sanho/state.json` checksum을 비교하고, 종료 시에는 임시 PID 또는 임시
label만 정리한다.

## H04. linked worktree와 공유 clone (변형)

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
8. 한 worktree에서 `sanho clean --dry-run`과 `sanho clean -y`를 차례로 실행한다.
   다른 managed worktree가 출력에 표시되고 공유 hooks와 clone이 보존되는지,
   남은 worktree가 clone 재생성 없이 게시할 수 있는지 확인한다. 마지막 managed
   worktree를 clean할 때만 공유 hooks와 clone이 제거돼야 한다.

## H05. hook 소유권과 기존 hook 공존 (변형)

1. 폐기 가능한 세 clone을 준비한다: repository-local `.githooks`, 사용자 전역
   또는 저장소 외부 공유 경로, Husky 9의 `.husky/_`. 기존 hook의 byte, mode,
   tracked 상태와 Husky shim hash를 기록한다.
2. local custom clone에서 플래그 없이 `sanho init`을, v0.1 Husky clone에서
   플래그 없이 `sanho migrate`를 실행한다. 둘 다 custom 경로를 이름지어
   거절하고 config, registry, clone, docs, backup, hook을 한 글자도 바꾸지
   않아야 한다.
3. local custom clone을 `sanho init --manage-custom-hooks ...`로 초기화한다.
   `.sanho.json`이 `hook_mode: custom`과 worktree-relative `hook_dir`을 기록하고,
   기존 내용과 mode를 보존하면서 portable `sanho hook ...` 6종을 정확히 한 번
   설치하는지 확인한다. 실제 commit과 push가 hook을 거쳐 성공해야 한다.
4. tracked `.husky/*`에 v0.1 7종 호출과 별도 lint 명령을 둔 Husky 9 clone을
   `sanho migrate --manage-custom-hooks`로 전환한다. 모든 기존 script backup이
   먼저 생기고, `.husky/_` hash는 불변이며, lint 명령은 남고 portable v2 6종만
   실행되어야 한다. 각 backup이 `git check-ignore`에 잡히되 그대로 존재해야
   한다. commit과 push로 인자 전달과 게시까지 확인한다.
5. managed hook에서 한 줄을 지우고 v0.1 pre-push 무인자형을 중복시킨 뒤
   `sanho doctor`와 `sanho doctor --fix`를 실행한다. 누락·legacy·중복을 보고한
   뒤 정확히 한 v2 줄로 복구하고 남의 실패 status를 성공으로 덮지 않아야 한다.
6. 초기화 뒤 `core.hooksPath`를 다른 local 디렉터리로 바꾸고 `doctor --fix`를
   실행한다. 설정과 불일치를 경고하고 이전·새 경로 모두 수정하지 않아야 한다.
7. 전역·저장소 외부·symlink custom 경로에는
   `--manage-custom-hooks`를 줘도 모든 workspace 상태 쓰기 전에 거절해야 한다.
8. `sanho clean -y` 후 Sanho 줄만 제거되고 custom/Husky 내용과 mode가 그대로
   남는지 확인한다. Sanho가 만든 `#!/bin/sh` 껍데기만 삭제하고 사용자가 만든
   다른 shebang/comment-only script는 보존해야 한다.

## H06. SSH·network 실패와 재시도 (유지)

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

## H07. canonical 이력 rewrite 후 복구 (신설)

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

### H07을 force 없이 로컬에서 실행하는 방법

공유 canonical을 force-push하지 않아도 같은 Git 상태를 만들 수 있다. case마다
서로 독립된 application remote, canonical bare 저장소, `SANHO_HOME`을 만들고
canonical URL에는 한 실행에서만 쓰는 로컬 경로를 사용한다. 먼저 원래
canonical을 통해 workspace를 초기화하고 docs를 두 번 이상 게시한 뒤, 아직
push하지 않은 로컬 docs commit을 하나 만든다.

case A replacement는 원래 canonical의 최종 checkout을 orphan branch의 root
commit으로 다시 commit한다. 교체 전에 두 tree가 같은지 반드시 확인한다.

```bash
git clone "$case_root/canonical.git" "$case_root/rewrite-work"
git -C "$case_root/rewrite-work" checkout --orphan rewritten-main
git -C "$case_root/rewrite-work" commit -m 'docs: squash canonical history'
git -C "$case_root/rewrite-work" branch -M main

old_tree=$(git --git-dir="$case_root/canonical.git" rev-parse 'main^{tree}')
new_tree=$(git -C "$case_root/rewrite-work" rev-parse 'main^{tree}')
test "$old_tree" = "$new_tree"
git clone --bare "$case_root/rewrite-work" "$case_root/replacement.git"
```

case B replacement는 새 저장소에서 unrelated root commit을 만든다. 원래
canonical에는 없던 파일도 하나 넣어 이후 absorption 검증에 사용하고, tree가
실제로 다른지 확인한다. `master` 안내 검증은 처음부터 initial branch가
`master`뿐인 별도 case로 반복한다.

replacement가 준비된 뒤 Sanho process가 실행 중이지 않을 때 같은 filesystem
안에서 두 경로를 rename한다. application config의 URL은 바꾸지 않는다.

```bash
mv "$case_root/canonical.git" "$case_root/canonical.original.git"
mv "$case_root/replacement.git" "$case_root/canonical.git"
```

이것은 push가 아니므로 force option이나 외부 ref 변경이 없다. 원본 bare
저장소도 그대로 보존된다. 교체 사이에는 canonical 경로가 잠시 없으므로 공유
저장소나 동시에 사용되는 fixture에는 적용하지 않는다.

case B의 거절 전후에는 application remote와 replacement canonical의 head를
기록해 둘 다 그대로인지 확인한다. 출력된 후보 명령은 직접 고쳐 쓰지 말고
그대로 실행한다.

```bash
git -C <출력된-private-clone> log --oneline refs/remotes/origin/main
# master-only case에서는 refs/remotes/origin/master
```

새 canonical 파일 누락 변형은 다음처럼 publication 경계까지 실행한다.
`sync --continue`는 누락을 사용자가 선택한 해소로 받아들일 수 있지만, 이어지는
push의 absorption warrant는 이를 거절하고 양쪽 remote를 그대로 보존해야 한다.
누락 파일을 되살려 같은 resolution commit을 amend하면 post-rewrite hook이 옛
trailer에서 base를 다시 유도할 수 있으므로, 새 head를 다시 명시한 뒤 push한다.

```bash
git add docs/ && git commit --amend --no-edit
sanho sync --rebase-onto <새-canonical-head>
git push
```

이 복구는 dummy commit을 추가하지 않는다. 마지막에는 canonical이 replacement의
모든 파일과 로컬 파일을 포함하는지, `.sanho_base.json`이 새 head/tree를
가리키는지, `git rev-list --min-parents=2 --count <branch>`가 0인지 확인한다.
case A, case B, master-only case 사이에서 `.sanho_base.json`이나 private clone을
되감아 재사용하지 말고 fixture 전체를 새로 만든다.

## H08. symlink·file mode·binary 왕복 (신설)

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
5. 아주 긴 한 줄 파일 뒤에 아래 **완전한 순서의 marker trio**를 넣고 commit을
   시도해 탐지되는지 확인한다. 시작 marker 하나만으로는 충돌이 아니며, v0.1은
   64 KiB 이후의 완전한 trio도 보지 못했다.
   ```text
   <<<<<<< sanho-ours
   ours
   =======
   theirs
   >>>>>>> sanho-upstream
   ```
6. 10 MiB를 넘는 텍스트 파일을 docs에 두고 push한다. 조용히 통과하지 않고
   "너무 커서 스캔할 수 없다"는 오류로 게이트가 fail-closed인지 확인한다.
7. 양쪽에서 symlink를 서로 다른 대상으로 바꿔 `sanho sync` 충돌을 만들고, 해소
   후 결과가 정상 symlink인지 확인한다.

어느 단계에서든 파일이 조용히 사라지거나 일반 파일로 바뀌면 FAIL이다.

## H09. 설치 binary와 PATH 경계 (변형)

내부 함수를 직접 호출하지 않고 공개 설치 binary와 실제 git 명령만 사용한다.

1. 임시 `GOBIN`에 검증할 release를 설치하고 `command -v sanho`,
   `sanho version --json`, `go version -m`을 기록한다. checkout에서 빌드한
   binary와 섞지 않는다.
2. 그 binary로 새 작업공간을 init하고 정상 흐름을 한 바퀴 돈다.
3. 기본 hook이 release binary의 canonical 절대 경로를 shell 인용해 기록했는지
   확인한다. GUI Git 클라이언트에서 PATH를 비운 채 commit과 push를 실행해도
   같은 binary가 실행되어야 한다. 그 binary를 임시로 치우면 commit/post hook은
   fail-open, `pre-push`는 fail-closed인지 확인한 뒤 즉시 복원한다. H05의
   opt-in custom/Husky hook만 portable `sanho` 호출을 쓰며, PATH에서 binary를
   치웠을 때도 commit/post는 통과하고 pre-push는 막혀야 한다.
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
- 임시 branch, clone, `GOBIN`, `SANHO_HOME`, 자격 증명 설정을 정리했다.
- 유지한 validation 파일과 commit은 소유 저장소와 유지 이유가 기록돼 있다.
- 전체 release diff와 자동·hands-on 증적을 사용자에게 먼저 제출하고, 사용자가
  해당 결과를 검토한 뒤 별도의 명시적 최종 릴리스 승인을 제공했다. 구현·검증
  지시나 과거의 일반적인 릴리스 요청은 이 최종 승인으로 간주하지 않는다.
