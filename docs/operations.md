# Sanho 운영 가이드

설치, 업그레이드, 제거의 소유권과 절차는 [배포 규칙](deployment.md)을 따른다.
구조와 계약은 [아키텍처](architecture.md)가 권위 문서다.

## 빌드와 실행

```bash
make cli-build     # bin/sanho
make cli-install   # go install ./cmd/sanho
make install       # cli-install의 별칭
sanho version
```

공개 release는 저장소에서 직접 설치한다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.0
```

실행 파일은 `sanho` 하나다. daemon, socket, service 등록, frontend build,
hot-reload 도구는 없다.

런타임 경로를 바꾸는 환경 변수는 하나뿐이다.

| 변수 | 기본값 | 설명 |
|---|---|---|
| `SANHO_HOME` | `~/.sanho` | 레지스트리와 잠금 파일을 두는 디렉터리. 절대 경로여야 한다. |

canonical clone은 sanho home이 아니라 각 작업공간의
`<git-common-dir>/sanho/canonical`에 있다. 작업공간마다 하나이므로 서로 이름이
충돌할 수 없다. `.sanho*` 파일과 인증 정보는 commit하지 않는다(`sanho init`이
`.gitignore`에 항목을 넣는다).

## 일상 흐름

### 1. 상태 확인

```bash
sanho status              # 캐시된 canonical 기준
sanho status --refresh    # 먼저 fetch한 뒤 보고
sanho status --json
```

출력은 다음 모양이다.

```text
workspace : /path/to/app
project   : product
docs repo : git@github.com:example/example-docs.git (branch main)
base      : 67c4bbfeada3
canonical : 9a41f2cbbbbb
data      : canonical data is 3 minutes old
relation  : behind 2, ahead 0
sync      : 2 behind — 'sanho sync' will merge cleanly
```

committed docs가 마지막 publication base와 다르면 canonical 관계와 별도로 다음
줄이 나온다.

```text
publish   : committed docs changes are pending publication
```

hook이 실행되지 않은 채 app 저장소 push만 끝난 경우에도 이 줄이 남는다. 이때
같은 tip으로 `git push`만 반복하면 ref update가 없어 pre-push 게시가 실행되지
않는다. `sanho doctor --fix`로 hook을 복구한 뒤 안내대로 새 docs-changing
commit을 만들고 push한다.

`data` 줄은 언제나 캐시 나이를 말한다. 24시간을 넘기면 갱신 방법도 함께
말한다.

```text
data      : canonical data is 3 days old — run 'sanho status --refresh'
data      : canonical has never been fetched — run 'sanho status --refresh'
```

canonical에 아직 commit이 없으면 관계와 sync 예측을 계산하지 않는다.

```text
canonical : (no commits yet — your first push will publish docs)
```

base가 clone 안에서 해석되지 않으면 숫자를 지어내지 않는다.

```text
relation  : unknown (the recorded base is not in the canonical clone)
```

### 2. behind 경고를 만나면 sync

commit할 때 base가 뒤처져 있으면 `pre-commit`이 한 줄을 출력한다. commit은
그대로 성공한다.

```text
sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly
sanho: docs base is 2 commits behind — 'sanho sync' will report conflicts in docs/api.md; syncing sooner keeps them small
sanho: docs base is 2 commits behind — run 'sanho sync' to reconcile
sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly (canonical last checked 3 days ago)
```

최신 상태면 아무것도 출력하지 않는다. 침묵이 정상 신호다.

경고를 보면 `sanho sync`를 실행한다. 사전 조건은 docs 경로가 worktree와
index 양쪽에서 깨끗한 것이다. docs 밖의 작업 중인 변경은 건드리지 않는다.

```bash
sanho sync
```

깨끗하게 병합되면 사용자 이름으로 된 일반 commit 하나가 생긴다.

```text
sanho: synced docs to 9a41f2cbbbbb (commit 3f0d1a5c7e21)
```

병합 결과가 현재 docs와 같으면 commit할 것이 없으므로 base만 옮긴다.

```text
sanho: docs base advanced to 9a41f2cbbbbb (docs unchanged)
```

할 일이 없으면 그렇게 말한다.

```text
sanho: docs are up to date with 9a41f2cbbbbb
sanho: canonical repository has no commits yet; nothing to sync
```

docs가 dirty하면 거절한다. sync는 사용자 속도로 실행되므로 물어보는 편이
맞고, 이 요구 조건 덕분에 v0.1의 4겹 보존 기계가 통째로 사라졌다.

```text
sanho: docs have uncommitted changes: commit or stash your docs changes first
```

### 3. 충돌 해소 루프

`sanho sync`가 충돌을 보고해도 **실패가 아니다.** 종료 코드는 0이고
`--json`의 `status`는 `conflicts`다.

```text
sanho: merged docs with upstream — 2 files have conflicts:
  docs/api.md
  docs/schema.md
Resolve the markers, then:  git add docs/ && git commit
Then complete the sync:     sanho sync --continue
To undo this sync:          sanho sync --abort
```

마커는 임시 경로가 아니라 이름으로 표시된다.

```text
<<<<<<< sanho-ours
내 편집
=======
상류 편집
>>>>>>> sanho-upstream
```

해소는 완전히 표준 git 관용구다. **끝났다는 선언만 Sanho의 명령이다.**

```bash
$EDITOR docs/api.md docs/schema.md
git add docs/
git commit -m "docs: resolve sync conflicts"
sanho sync --continue
```

```text
sanho: sync completed; docs base is now 9a41f2cbbbbb
```

`--continue`가 하는 일은 둘뿐이다. sync note를 지우고, docs base를 병합 대상으로
옮긴다. commit을 만들지 않고 network를 열지 않으므로 오프라인에서도 끝낼 수 있다.

해소가 끝나기 전에 commit하면 막힌다. 안내는 같은 세 선택지를 반복한다.

```text
sanho: a sync is in progress — 1 files still have conflicts:
  docs/api.md
Resolve the markers, then:  git add docs/ && git commit
Then complete the sync:     sanho sync --continue
To undo this sync:          sanho sync --abort
```

`--continue`를 너무 일찍 실행해도 같은 이야기를 한다.

```text
sanho: the sync is not ready to be completed (the docs worktree still contains conflict markers: docs/api.md)
Finish the resolution with 'git add docs/ && git commit', then run 'sanho sync --continue' again.
Or run 'sanho sync --abort' to undo the sync.
```

sync 중이 아닌데 staged docs에 마커가 있으면 별도 메시지로 막는다.

```text
sanho: staged docs contain conflict markers:
  docs/api.md
Resolve them, then 'git add' the files and commit again.
```

**commit만으로는 sync가 끝나지 않는다.** 해소를 commit한 뒤 `--continue` 전까지
push는 거절되며, 그 사실을 그대로 말한다.

```text
sanho: the sync from 67c4bbfeada3 to 9a41f2cbbbbb is not completed — the resolution is committed, and only 'sanho sync --continue' records it
Run 'sanho sync --continue' now, or 'sanho sync --abort' to forget the sync — your commits stay; only the recorded base returns to its pre-sync value.
```

충돌 sync 자체는 base를 옮기지 않는다. 마커가 worktree에 있는 동안 docs는 여전히
병합 이전 상태에서 파생돼 있기 때문이고, base를 대상으로 옮기는 것은
`--continue` 하나뿐이다.

마커를 stash하거나 `git checkout HEAD -- docs`로 치워 두면 끝난 것처럼 보이지만
끝난 것이 아니다. commit할 때마다 이렇게 알린다(막지는 않는다). push는 거절한다.

```text
sanho: the sync from 67c4bbfeada3 to 9a41f2cbbbbb is not completed; no commit has changed the files it conflicted on
Run 'sanho sync --abort' to undo it — anything you stashed stays in your stash — then 'sanho sync' to lay the conflicts out again.
If the docs already read the way you want them, run 'sanho sync --continue' instead to complete the sync as it stands.
```

마지막 줄이 **"전부 내 것으로"(take ours) 해소의 출구**다. 모든 충돌 경로를
바이트 그대로 두는 해소는 아무 흔적도 남기지 않아서 도구가 알아볼 방법이 없고,
그래서 예전에는 abort하고 다시 sync하는 길밖에 없었다. 지금은 docs가 원하는
모양이면 `--continue`로 그대로 끝내면 된다.

창이 열려 있는 동안 신선도 경고(`docs base is N commits behind`)와
`sanho status`의 behind 줄은 이 안내로 대체된다. 창 안에서 `sanho sync`는
거절되므로, 그 상태에서 `sanho sync`를 안내하는 줄은 출력되지 않는다.

되돌리고 싶으면 언제든 abort할 수 있다.

```bash
sanho sync --abort
```

```text
sanho: sync aborted; docs restored to HEAD
```

### 4. push 거절 대응

게시는 `git push` 시점에만 일어난다. 성공하면 한 줄이 나온다.

```text
sanho: published docs 9a41f2cbbbbb (fast_forward)
```

case 이름은 `up_to_date`, `fast_forward`, `auto_merge`, `unknown_base`다.
`auto_merge`는 상류와 자동으로 합쳐 게시했다는 뜻이며 push는 그대로
계속된다. 이때 worktree는 건드리지 않으므로 `status`가 "behind(내 병합
결과)"로 보인다. `sanho pull`이 따라잡는다.

실제 게시 뒤에는 private canonical clone에 대해 Git의 자동 gc를 한 번
요청한다. 임계값에 미달하면 Git이 아무 작업도 하지 않으며, gc 실패도 이미
완료된 게시나 애플리케이션 push를 되돌리지 않는다. 평소에는 별도 출력이 없고,
`sanho --verbose hook pre-push`처럼 verbose 진단을 켠 경우에만 다음 형태로
남는다.

```text
sanho: debug: private canonical clone maintenance skipped: canonical: maintain private clone …
```

게시할 docs 변경이 없는 push에서는 자동 gc도 실행하지 않는다.

거절은 다음 다섯 가지다. 어느 경우에도 원격 ref는 하나도 바뀌지 않는다.

**(a) docs 충돌.**

```text
sanho: your docs changes conflict with upstream (base 67c4bbfeada3 → 9a41f2cbbbbb)
  docs/api.md
Run 'sanho sync', resolve, commit, then push again.
error: push rejected — no remote ref was changed
```

충돌한 파일 목록은 거절이 이미 알고 있던 정보다. 목록을 빼면 어느 파일인지
알아내려고 `sanho sync`를 한 번 돌려 봐야 했다. 파일 단위 정보가 없는
거절(CAS 재시도 소진 등)은 목록 줄 없이 그대로 출력된다.

**(b) 화해가 필요한 그 밖의 상태.** 사유는 `no_base`(기록된 base 없음)
또는 `cas_retry_exhausted`(경합에서 3번 연속 패배)다.

```text
sanho: docs must be reconciled before publishing (no_base; base (none) → 9a41f2cbbbbb)
Run 'sanho sync', resolve if needed, commit, then push again.
error: push rejected — no remote ref was changed
```

**(c) commit된 충돌 마커.**

```text
sanho: pushed docs still contain conflict markers:
  docs/api.md
resolve the markers before pushing
error: push rejected — no remote ref was changed
```

**(d) sync 미완료.**

```text
sanho: finish the sync first: resolve the conflicts, 'git add' and 'git commit', then 'sanho sync --continue' (or 'sanho sync --abort' to undo it)
error: push rejected — no remote ref was changed
```

**(e) canonical 도달 불가 / 이력 rewrite.** 아래 장애 대응 절을 본다.

어떤 경우에도 `--no-verify`, force push, hook 파일 수정으로 우회하지 않는다.
안내된 명령을 실행한 뒤 **같은 `git push`를 다시 실행**하면 된다.

### 5. 소비만 할 때는 pull

로컬 docs 편집이 전혀 없을 때 canonical을 그대로 받아온다.

```bash
sanho pull
sanho pull --commit   # 갱신을 'docs: sync to <oid>' commit으로도 기록
```

```text
sanho: pulled docs to 9a41f2cbbbbb
sanho: pulled docs to 9a41f2cbbbbb (commit 3f0d1a5c7e21)
```

로컬 편집이 있으면 덮어쓰지 않고 거절한다.

```text
sanho: local docs have changes that 'sanho pull' cannot fast-forward: local docs differ from base 67c4bbfeada3; run 'sanho sync' to reconcile them
sanho: local docs have changes that 'sanho pull' cannot fast-forward: no docs base is recorded; run 'sanho sync' first
```

### 6. 다른 작업공간 확인

```bash
sanho state           # 현재 작업공간의 프로젝트만
sanho state --all     # 등록된 전부
sanho status          # siblings 표 포함
```

sibling 표의 관계 값은 `same`, `ahead N`, `behind N`, `diverged`,
`unknown`이다. 레지스트리 항목은 다른 checkout이 **보고한 값**이므로, 이
clone이 해석하지 못하는 commit은 추측 대신 `unknown`으로 남는다.

## 장애 대응

### canonical 도달 불가

읽기 경로와 쓰기 경로가 다르게 동작한다. 이 비대칭은 의도된 것이다.

- **읽기**(`status`, `state`, `pre-commit` 감지): 마지막 fetch 결과를 그대로
  제공하고, 데이터 나이를 명시한다. **오프라인 commit은 언제나 성공한다.**
  코드만 바꾼 commit도, docs를 바꾼 commit도 마찬가지다. commit 경로는
  network 연결을 아예 열지 않는다.
- **쓰기**(`sync`, `pull`, `pre-push` 게시): fetch가 성공해야 하며 실패하면
  fail-closed다. push는 이미 network 접근을 전제하기 때문이다.

```text
sanho: canonical repository unreachable (git@github.com:example/example-docs.git): <git이 보고한 한 줄>
Check network access to the docs repository, then push again.
error: push rejected — no remote ref was changed
```

확인 순서는 SSH key, 저장소 URL, network, branch 권한이다.

```bash
git ls-remote <docs-repo-url> HEAD
sanho doctor
```

Sanho는 프롬프트를 띄우지 않는다. network 작업에는
`ssh -o BatchMode=yes -o ConnectTimeout=10`을 강제하므로 자격 증명이 없으면
기다리지 않고 즉시 실패한다. 조사 중에도 `.git/sanho/canonical` 안에서 임의
commit이나 force push를 만들지 않는다.

### sync 진행 중

`sync.json`이 남아 있는 동안에는 게시가 막히고, `sync`와 `pull`도 거절한다.

```text
sanho: a conflicted sync is in progress (syncing 67c4bbfeada3 to 9a41f2cbbbbb)
Resolve the markers and commit, then run 'sanho sync --continue' — or 'sanho sync --abort' to undo it.
```

`sanho doctor`도 같은 상태를 경고로 보고한다.

```text
[warn] sync             a sync from 67c4bbfeada3 to 9a41f2cbbbbb is unresolved — complete it with 'sanho sync --continue', or undo it with 'sanho sync --abort'
```

선택지는 둘뿐이고 둘 다 반드시 성공한다. 마커를 해소해 commit하거나,
`sanho sync --abort`로 되돌린다. `.git/sanho`를 손으로 지우지 않는다.

`sanho clean`도 이 상태에서는 거절한다. 마커투성이 docs worktree를 설명할
note 없이 남기게 되기 때문이다.

```text
sanho: a conflicted sync is in progress; complete it with 'sanho sync --continue', or undo it with 'sanho sync --abort' first
```

note가 없는데 abort를 실행하면 그렇게 말한다.

```text
sanho: no sync is in progress
```

### canonical 이력 rewrite

기록된 base가 canonical 게시 branch에서 더 이상 도달할 수 없는 상태다. push는
먼저 `docs-base-tree`와 같은 docs tree를 가진 commit을 canonical 이력에서
찾아 스스로 재유도를 시도한다. 성공하면 사용자는 아무것도 하지 않아도 된다.

재유도에 실패하면, 실행 가능한 명령이 있을 때만 명령을 안내한다.

```text
sanho: canonical history was rewritten; base 67c4bbfeada3 is no longer reachable
Run 'sanho sync --rebase-onto 1111111111111111111111111111111111111111', resolve if needed, commit, then push again.
error: push rejected — no remote ref was changed
```

어떤 canonical commit도 그 docs tree를 갖고 있지 않으면 실패할 명령을
안내하는 대신 후보를 고르는 방법을 알려 준다.

```text
sanho: canonical history was rewritten; base 67c4bbfeada3 is no longer reachable
manual intervention required: no canonical commit carries this workspace's docs base tree.
List the candidates with:  git -C /path/to/app/.git/sanho/canonical log --oneline refs/remotes/origin/main
Then run:                  sanho sync --rebase-onto <commit>
error: push rejected — no remote ref was changed
```

조회 명령이 `origin/HEAD`가 아니라 게시 branch 이름을 그대로 쓰는 것은
중요하다. private clone은 `git init --bare` + fetch로 만들어지므로
`refs/remotes/origin/<branch>`만 있고 `refs/remotes/origin/HEAD`는 없다.
HEAD를 지목하는 명령은 그 명령이 출력되는 바로 그 상태에서 실패한다.

`--rebase-onto`로 충돌을 해소하고 commit한 뒤 `sanho sync --continue`를
완료하면 추가적인 dummy commit 없이 바로 push한다. 해소 tip이 새 canonical
head의 모든 문서를 이미 포함하면 Sanho가 content absorption을 증명해, 해소
commit trailer가 rewrite 전 base를 가리키더라도 안전한 fast-forward로
게시한다.

`sanho sync`가 같은 상태를 만나면 다음처럼 거절한다.

```text
sanho: the recorded docs base is unknown to the canonical repository: neither 67c4bbfeada3 nor its docs tree 2f41ab90c3d2 is in canonical history; pick a canonical commit and run 'sanho sync --rebase-onto <commit>'
sanho: the recorded docs base is unknown to the canonical repository: 67c4bbfeada3 carries no docs-base-tree to re-anchor by; pick a canonical commit and run 'sanho sync --rebase-onto <commit>'
```

존재하지 않는 대상을 지정하면 거절한다.

```text
sanho: the requested target is not a canonical commit: deadbeefdead
```

자세한 절차는 [복구 가이드](recovery.md)를 따른다.

### base 파일 손상 또는 유실

`sanho doctor`가 진단하고 `--fix`가 고친다. 복구는 완전히 로컬이다. commit
이력의 trailer를 다시 읽는 것이 전부이므로 network도 canonical도 필요 없다.

```bash
sanho doctor
sanho doctor --fix
```

```text
[warn] base             no docs base is recorded — run 'sanho doctor --fix' to re-derive the base from commit history
[warn] base             the recorded base is not a valid OID pair — run 'sanho doctor --fix' to re-derive the base from commit history
[warn] base             the base file is unreadable: <원인> — run 'sanho doctor --fix' to re-derive the base from commit history
```

성공하면 다음과 같다.

```text
[ok  ] base-fix         re-derived the base as 67c4bbfeada3 from commit history
```

이력에 stamp된 commit이 하나도 없으면 재유도할 근거가 없다.

```text
[warn] base-fix         no commit in the last 500 carries a docs-base or docs-version trailer; run 'sanho sync' to establish a base
```

trailer가 지워지는 흔한 경로는 메시지만 바꾸는 `git commit --amend -m`이다.
다음 commit이 stamp 규칙 2번(HEAD의 docs tree가 HEAD~와 다름)으로 자동
복원하며, 그 전에도 `sanho doctor --fix`로 즉시 복원할 수 있다. trailer는
기록이자 복구원이지 gate 입력이 아니므로 유실이 작업을 막지 않는다.

```text
sanho: docs provenance not stamped (no docs base is recorded); run 'sanho doctor --fix' to restore it
```

### 레지스트리 잠금과 손상

레지스트리(`~/.sanho/state.json`)는 관찰용이다. 게시 정확성은 여기에 의존하지
않으므로 갱신 실패가 성공한 작업을 되돌리지 않는다.

다른 Sanho 프로세스가 잠금을 쥐고 있으면 5초 후 포기한다.

```text
[warn] registry         the registry lock is not available: another sanho process holds /Users/name/.sanho/state.lock; retry in a moment
```

잠시 후 다시 시도한다. 잠금 파일을 지우거나 우회하지 않는다.

primary가 깨졌으면 `.bak`에서 자동 복구하고 primary도 되살린다. 둘 다 읽을 수
없을 때만 오류이며, 이 경우 **빈 상태로 시작하지 않는다**.

```text
sanho: registry state is unreadable: primary /Users/name/.sanho/state.json is corrupt (<원인>) and backup /Users/name/.sanho/state.json.bak is also corrupt (<원인>)
```

두 파일을 먼저 다른 곳에 보존한 뒤 JSON을 조사한다. 빈 파일로 덮어써서 문제를
숨기지 않는다. 레지스트리는 재구성 가능한 상태이므로, 조사 후 각 작업공간에서
`sanho init`(또는 `sanho sync`/`sanho status`)을 한 번씩 실행하면 항목이 다시
채워진다.

### v0.1 강등 상태

v0.2 binary를 설치했지만 아직 `sanho migrate`를 실행하지 않은 작업공간이다.
hook 라인이 `sanho`를 이름으로 호출하므로 강등은 자동으로 시작된다.

```text
sanho: this workspace uses the v0.1 layout; run 'sanho migrate'
```

- `git commit`은 계속 성공한다. hook은 위 힌트만 출력하고 통과한다.
- `git push`는 위 힌트와 함께 fail-closed로 막힌다. push 경계가 migration을
  촉구하는 지점이다.
- `sanho status`를 비롯한 명령은 exit 1로 거절한다.
- `sanho clean`은 이 상태에서도 동작한다.
- `sanho migrate`가 이 상태에서 성공하는 유일한 명령이다.

migration 절차는 [배포 규칙](deployment.md)의 "v0.1 → v0.2 업그레이드" 절에
있다.

### 작업공간이 아닌 곳

```text
sanho: not a sanho workspace (no .sanho.json here); run 'sanho init' to create one
```

작업공간 판정은 **현재 디렉터리**에 `.sanho.json`이 있는지로만 한다. 상위
디렉터리를 거슬러 올라가지 않는다. hook은 언제나 worktree 최상위에서
실행되므로, 올라가면 의도치 않은 작업공간을 건드릴 수 있기 때문이다.
`sanho state`는 예외적으로 작업공간 밖에서도 동작한다. 레지스트리는 작업공간
바깥에 있기 때문이며, 이때는 범위를 좁힐 프로젝트와 head를 읽을 clone이 없다.

## 진단

```bash
sanho doctor
sanho doctor --json
```

검사 항목은 `git`, `workspace-config`, `hooks`, `clone`, `canonical-head`,
`base`(필요 시 `base-fix`), `registry`, `sync`, `docs`다. 경고를 찾아도 종료
코드는 0이다. 문제를 찾을 때마다 실패하는 진단 명령은 문제 조사에 쓸 수 없다.
검사 자체를 실행할 수 없을 때만 1이다.

hook 문제는 재설치를 안내한다.

```text
[warn] hooks            pre-push: missing; post-merge: installed 2 times — run 'sanho doctor --fix' to reinstall them
[warn] hooks            pre-push: carries v0.1 lines — run 'sanho doctor --fix' to reinstall them
```

hook을 실제로 복구한 뒤 local HEAD docs가 publication base와 다르면 다음 경고가
추가된다.

```text
[warn] publication      committed docs differ from the publication base after hook repair; make another docs-changing commit, then run 'git push' to publish them
```

clone 문제는 다음과 같다.

```text
[warn] clone            the canonical clone is missing (/path/.git/sanho/canonical) — run 'sanho sync' to recreate it
[warn] clone            origin is <A> but the workspace config says <B>
```

`docs` 검사는 파일 수, 바이트, symlink 수를 보고한다. symlink는 v0.1의 tar
snapshot 전송에서 조용한 데이터 유실 원인이었으나, v0.2는 내용을 git object로
옮기므로 이제 경고가 아니라 목록 정보다.

## 검증

```bash
make test
```

`make test`는 `test-prepare`, `test-unit`, `test-int`, `test-e2e`를 이 순서로
실행한다.

| target | 내용 |
|---|---|
| `test-prepare` | `go generate`, `go fmt`, `go mod verify`, `docs-check`, 패키지 소유권 검사, 아키텍처 guardrail, `go vet`, `golangci-lint` |
| `test-unit` | 모든 단위 패키지를 `-race`로 실행 |
| `test-int` | `bin/sanho`를 빌드해 `SANHO_CLI_BINARY`로 넘기고 `test/cli/integration`과 `test/docsync`를 실행 |
| `test-e2e` | `test/cli/e2e` — 시나리오 매트릭스(S1~S11), 프로세스 수준 동시성, guidance closure 스위트 — 와 `test/install`(`go install ./cmd/sanho` 확인) |
| `test-scale` | `SANHO_SCALE=1 make test-scale`로만 실행하는 선택적 대형 저장소 correctness profile. 1,000 files, 500 commits, 약 50 MiB에서 init·status·push·sync 시간을 기록하며 고정 지연 임계값은 두지 않는다. `make test`에는 포함되지 않는다. |
| `docs-check` | 필수 문서 존재 여부와 폐기된 참조 문자열 검사 |
| `test-package-ownership` | `go list` 결과와 `UNIT_PACKAGES` 목록이 일치하는지 검사 |
| `test-architecture` | usecase↔infra 계층 규칙 |

통합 테스트의 docs 저장소에는 실제 운영 저장소 대신 임시 디렉터리의 폐기
가능한 bare Git 저장소를 사용한다.

`test/cli/e2e`에는 빌드된 binary를 블랙박스로 구동하는 guidance closure
스위트와 시나리오 스위트가 있다. 카탈로그 항목마다 실제 작업공간을 그 상태로
만들고, 메시지가 실제로 나오는지 확인한 뒤, 안내된 명령을 그 상태에서 그대로
실행해 성공을 요구한다. 어느 target에서 실행하든 `SANHO_CLI_BINARY`로 검사
대상 binary를 지정해야 한다. 프로세스를 띄우는 스위트이므로 `-race` 없이
실행하며, `-race`는 in-process 스위트(`test-unit`, `test-int`)가 담당한다.

안내 문자열을 카탈로그에 등록하지 않으면 `test-unit`이 실패한다. 메시지
파일을 소스로 파싱해 등록 누락을 잡는 검사가 `internal/interface/cli`
단위 테스트에 들어 있기 때문이다.

실제 Git hosting, 인증, branch rule, 여러 머신을 포함한 릴리스 검증은
[hands-on 체크리스트](hands-on-testing.md)에 따라 별도로 실행한다. 자동 gate와
hands-on 증적이 모두 준비돼도 release diff와 결과를 사용자에게 먼저 제출해야
한다. 사용자가 그 결과를 검토한 뒤 별도로 명시한 최종 승인 없이는 commit,
push, tag, GitHub Release 또는 사용자 binary 설치를 진행하지 않는다. 구현이나
검증을 수행하라는 지시는 릴리스 승인으로 해석하지 않는다.
