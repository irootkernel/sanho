# Sanho 복구 절차

이 문서는 v0.2에서 복구가 필요한 상황과 지원되는 절차를 정리한다. 실제
저장소에 적용하기 전에 branch 이름, local/remote OID, `sanho status --json`
출력을 기록한다. `--no-verify`, 무조건 force push, `.git/sanho` 직접 삭제,
hook 파일 수동 편집은 사용하지 않는다.

v0.2에서 복구 부담은 크게 줄었다. 도구가 애플리케이션 저장소에 commit을
만들지 않고, ref를 조작하지 않으며, 여러 단계 transaction을 유지하지 않기
때문이다. 중단 지점에서 남을 수 있는 상태는 **충돌 sync 진행 중** 하나뿐이고,
그 상태를 되돌리는 명령은 구조적으로 실패할 수 없다.

## 우선 확인

```bash
git status
sanho status
sanho status --json
sanho doctor
```

`sanho doctor`는 경고를 찾아도 종료 코드가 0이므로 조사 도구로 바로 쓸 수
있다.

## 1. 충돌 sync를 되돌린다 — `sanho sync --abort`

**적용 상황**: `sync.json`이 남아 있는 모든 상태. `sanho status`의
`sync : IN PROGRESS`, `sanho doctor`의 `[warn] sync`, push 거절 메시지
`finish the sync first`가 모두 같은 상태를 가리킨다.

```bash
sanho sync --abort
```

```text
sanho: sync aborted; docs restored to HEAD
```

### 이 명령이 항상 성공하는 이유

abort는 세 가지만 한다.

1. docs worktree와 index를 `HEAD` 기준으로 복원한다.
2. base 파일을 note에 기록된 `prev_base`로 되돌린다. sync 진입 시점에 base가
   없었다면 base 파일을 삭제한다.
3. note를 지운다.

ref를 움직이지 않고, commit을 만들지 않고, docs 밖의 파일을 건드리지 않는다.
따라서 "abort가 실패하는 상태"가 존재하지 않는다. 순서도 재실행 가능하도록
정해져 있다. note는 "abort가 아직 밀려 있다"는 증거이므로 **마지막에** 지우고,
그 앞 단계는 모두 멱등이다. 중간에 프로세스가 죽어도 `sanho sync --abort`를
다시 실행하면 그대로 이어진다.

note가 없는데 실행하면 그렇게 말할 뿐 아무것도 바꾸지 않는다.

```text
sanho: no sync is in progress
```

### 되돌리는 대신 끝내려면

해소는 표준 git 관용구다. 두 경로 모두 안전하고, 어느 쪽을 선택해도 된다.

```bash
$EDITOR docs/…            # <<<<<<< sanho-ours … >>>>>>> sanho-upstream 제거
git add docs/
git commit -m "docs: resolve sync conflicts"
```

## 2. canonical 이력이 rewrite된 경우 — `sanho sync --rebase-onto`

**적용 상황**: 기록된 base가 canonical 게시 branch에서 더 이상 도달할 수
없다. squash, rebase, filter 등으로 canonical 이력이 다시 쓰였을 때 생긴다.

먼저 Sanho가 스스로 재유도를 시도한다. `docs-base-tree`와 같은 docs tree를
가진 canonical commit을 찾으면 그것을 base로 채택하고 그대로 진행하므로,
대부분의 rewrite(내용을 보존하는 squash·rebase)에서는 사용자가 할 일이 없다.

자동 재유도가 실패했을 때만 아래로 넘어간다.

### 2-1. 안내가 commit을 지목한 경우

push 거절 메시지가 실행 가능한 명령을 그대로 준다.

```text
sanho: canonical history was rewritten; base 67c4bbfeada3 is no longer reachable
Run 'sanho sync --rebase-onto 1111111111111111111111111111111111111111', resolve if needed, commit, then push again.
error: push rejected — no remote ref was changed
```

```bash
sanho sync --rebase-onto 1111111111111111111111111111111111111111
# 충돌이 있으면 해소 → git add docs/ → git commit
git push
```

### 2-2. 지목할 commit이 없는 경우 — 수동 개입

어떤 canonical commit도 이 작업공간의 docs base tree를 갖고 있지 않으면
Sanho는 실패할 명령을 안내하지 않는다. 대신 후보를 고르는 방법을 알려 준다.

```text
sanho: canonical history was rewritten; base 67c4bbfeada3 is no longer reachable
manual intervention required: no canonical commit carries this workspace's docs base tree.
List the candidates with:  git -C /path/to/app/.git/sanho/canonical log --oneline refs/remotes/origin/main
Then run:                  sanho sync --rebase-onto <commit>
error: push rejected — no remote ref was changed
```

명령이 지목하는 ref는 `origin/HEAD`가 아니라 해석된 게시 branch
(`main`, 없으면 `master`)다. private clone은 `git init --bare` + fetch로
만들어져 `refs/remotes/origin/<branch>`만 갖고 `refs/remotes/origin/HEAD`는
아예 없으므로, HEAD를 지목하면 안내된 명령이 바로 그 상태에서 실패한다.

후보를 고르는 절차는 다음과 같다. `<branch>`는 `sanho status`가 보여 주는
게시 branch로 바꾼다.

```bash
CLONE="$(git rev-parse --path-format=absolute --git-common-dir)/sanho/canonical"

git -C "$CLONE" fetch origin
git -C "$CLONE" log --oneline refs/remotes/origin/main | head -40

# 내 docs와 가장 가까운 상태를 고른다. 내용 비교가 도움이 된다.
git -C "$CLONE" show --stat <후보-commit>
```

고른 commit은 "내 로컬 docs 편집이 어디에서 갈라졌는가"에 대한 답이어야 한다.
너무 뒤쪽(오래된) commit을 고르면 이미 반영된 상류 변경이 다시 충돌로
올라오고, 너무 앞쪽(최신) commit을 고르면 상류 변경이 조용히 내 편집으로
덮일 수 있다. 판단이 서지 않으면 보수적으로 오래된 쪽을 고르고 충돌을 손으로
읽는 편이 안전하다.

```bash
sanho sync --rebase-onto <고른-commit>
```

대상은 canonical에 실제로 존재해야 한다. 존재하지 않으면 거절한다.

```text
sanho: the requested target is not a canonical commit: deadbeefdead
```

canonical에 commit이 하나도 없는 저장소에 `--rebase-onto`를 지정한 경우도
마찬가지로 거절한다. 이때는 `--rebase-onto` 없이 push하면 첫 push가 canonical을
부트스트랩한다.

`--abort`와 `--rebase-onto`는 함께 쓸 수 없다.

```text
sanho: --abort and --rebase-onto cannot be combined
```

## 3. base 파일이 없거나 깨진 경우 — `sanho doctor --fix`

**적용 상황**: `.sanho_base.json`이 삭제됐거나, 내용이 OID 쌍으로 읽히지
않거나, 메시지만 바꾸는 `git commit --amend -m`으로 trailer가 지워진 뒤
파일까지 유실된 상태.

```bash
sanho doctor          # 진단만
sanho doctor --fix    # 이력에서 재유도
```

```text
[ok  ] base-fix         re-derived the base as 67c4bbfeada3 from commit history
```

복구는 완전히 로컬이다. `git log HEAD`를 최대 500개까지 거슬러 올라가며
`docs-base` / `docs-base-tree` / legacy `docs-version` trailer를 읽고, 최신순
첫 채택 가능 commit의 base를 쓴다. network도 canonical도 필요 없다. 이것이
v0.1과의 결정적 차이다. v0.1에서는 trailer 복구가 daemon 왕복을 요구했고, 그
왕복이 막혀 있으면 복구 자체가 불가능했다.

이력에 stamp된 commit이 하나도 없으면 재유도할 근거가 없다.

```text
[warn] base-fix         no commit in the last 500 carries a docs-base or docs-version trailer; run 'sanho sync' to establish a base
```

이때는 `sanho sync`가 base를 만들어 준다. base가 없는 sync는 empty tree를 병합
base로 삼으므로 양쪽 추가의 합집합이 되고, 같은 경로를 서로 다르게 추가한
곳에서만 충돌한다. 즉 안내는 반드시 성공한다.

base가 정상일 때는 다음처럼 보고한다.

```text
[ok  ] base             commit 67c4bbfeada3, tree 2f41ab90c3d2
```

### hook과 clone이 어긋난 경우

```bash
sanho init --force -y   # hook 재설치가 필요할 때
```

`sanho doctor`가 다음을 보고하면 재설치 대상이다.

```text
[warn] hooks            pre-push: missing; post-merge: installed 2 times — run 'sanho init --force' to reinstall
[warn] hooks            pre-push: carries v0.1 lines — run 'sanho init --force' to reinstall
[warn] clone            the private clone is missing (/path/.git/sanho/canonical) — run 'sanho init' in this workspace
```

hook 설치는 정확한 줄 일치로만 판단하고 남의 줄은 원문 그대로 보존하므로,
재설치가 custom hook 내용을 지우지 않는다. private clone은 Sanho 소유이며
언제든 다시 만들 수 있다. `sanho sync`와 `sanho pull` 같은 쓰기 경로는 clone이
없으면 스스로 만든다.

## 4. 수동 개입이 필요한 유일한 경우들

아래 네 가지 외에는 Sanho가 안내하는 명령으로 해결된다.

### 4-1. rewrite 후 anchor를 찾을 수 없음

위 2-2절. 사람이 canonical 이력에서 base를 골라야 한다.

### 4-2. 레지스트리 primary와 백업이 모두 손상

```text
sanho: registry state is unreadable: primary /Users/name/.sanho/state.json is corrupt (<원인>) and backup /Users/name/.sanho/state.json.bak is also corrupt (<원인>)
```

레지스트리는 **관찰용**이므로 게시 정확성과 무관하다. 절차는 다음과 같다.

```bash
mkdir -p ~/sanho-registry-incident
cp ~/.sanho/state.json     ~/sanho-registry-incident/   2>/dev/null || true
cp ~/.sanho/state.json.bak ~/sanho-registry-incident/   2>/dev/null || true

python3 -m json.tool ~/sanho-registry-incident/state.json      # 어디가 깨졌는지 확인
python3 -m json.tool ~/sanho-registry-incident/state.json.bak
```

두 사본을 보존한 뒤에만 문제 파일을 치운다. 빈 파일로 덮어써서 증상을 숨기지
않는다.

```bash
rm ~/.sanho/state.json ~/.sanho/state.json.bak
```

그 다음 각 작업공간에서 아래 중 하나를 실행하면 항목이 다시 채워진다.
레지스트리에 자기 항목을 기록하는 것은 `init`, `migrate`, `sync`, `pull`,
그리고 게시에 성공한 `pre-push`다. `status`와 `state`는 읽기만 하므로 항목을
되살리지 못한다.

```bash
sanho sync        # 또는 sanho pull — 성공한 뒤 자기 항목을 다시 기록한다
sanho init --project <name> --docs-repo-url <url> --force -y
```

canonical 저장소와 각 작업공간의 `.sanho_base.json`은 손상되지 않았으므로
문서 데이터는 손실되지 않는다.

### 4-3. v0.1 transaction이 살아 있는 상태에서의 migration

```text
sanho: a v0.1 pull-commit transaction or pending-fix state is still present; finish or abort it with the v0.1 binary, then run 'sanho migrate' again
```

v0.2는 v0.1의 transaction 상태를 해석하지 않는다. 반쯤 해석한 transaction이
v0.1의 최악 결함이었기 때문에, 추측하는 대신 멈춘다. v0.1 binary로 먼저
정리한다.

```bash
# v0.1 binary로 실행한다
sanho status                     # pull_commit 분류와 next_command 확인
sanho pull-commit --continue     # 또는 --abort / --recover
```

`.sanho_pending_fix` 파일이 남아 있는 경우도 같은 메시지로 막힌다. v0.1
binary로 해당 상태를 끝낸 뒤 다시 `sanho migrate`를 실행한다.

### 4-4. 기존 docs에 provenance가 전혀 없는 재사용 init

```text
existing docs directory has no docs-base/docs-version commits; commit docs through sanho first or rerun with --force to replace the directory
```

기존 docs가 무엇에서 파생됐는지 알 수 없으므로 Sanho는 base를 추측하지 않는다.
canonical head를 base로 삼으면 사실이 아닌 파생 관계를 주장하게 되고, 다음
push가 무관한 내용을 "병합"하게 된다. 선택지는 둘이다.

```bash
# (a) 기존 docs를 canonical 내용으로 대체한다 — 파괴적이다
sanho init --project <name> --docs-repo-url <url> --force -y

# (b) 기존 docs를 살린다: 먼저 base를 만들고 그 위에서 commit한다
sanho init --project <name> --docs-repo-url <url>   # 실패해도 무방
sanho sync                                          # base 없는 sync = 합집합 병합
git add docs/ && git commit -m "docs: adopt existing docs"
```

`--force`는 확인 없이 실행되지 않는다.

```text
sanho: --force replaces the existing docs directory with canonical content; rerun with -y to confirm
```

## 5. migrate 롤백

`sanho migrate`는 되돌릴 수 있도록 설계돼 있다. 레지스트리 변환은
`~/.sanho/state.json`을 v0.1 daemon 스키마에서 v2 레지스트리 스키마로
**제자리에서 덮어쓰고** 같은 내용을 `state.json.bak`에도 쓰기 때문에,
migrate는 변환 직전에 v0.1 원문을 `~/.sanho/state.json.v1.bak`으로
자동 보존한다(이미 존재하면 덮어쓰지 않으며, 두 번째 실행은 v2 상태를
보고 이 단계에 도달하지 않는다). 추가 안전을 원하면 수동 사본을 하나
더 떠 두어도 좋다.

```bash
cp ~/.sanho/state.json ~/.sanho/state.json.pre-v0.2   # 선택: 추가 안전용
```

### 5-1. migrate가 자동으로 만드는 백업

| 원본 | 백업 | 비고 |
|---|---|---|
| `~/.sanho/state.json` | `~/.sanho/state.json.v1.bak` | v0.1 daemon state 원문(레지스트리 변환 전) |
| `.sanho.json` | `.sanho.json.bak` | v0.1 설정 원문 |
| `.sanho_docs_hash` | `.sanho_docs_hash.bak` | 원본도 삭제하지 않고 그대로 남긴다 |

`.sanho_docs_hash`는 읽기 전용 호환 입력이므로 Sanho가 소비하지 않는다.
`.sanho_base.json`이 없을 때만 읽히므로, 롤백 시 그대로 다시 쓰인다.

migrate가 새로 만들거나 바꾸는 것은 다음과 같다.

- `.sanho.json`을 v2 스키마로 교체(`docs_repo_url` 추가, `socket_path` 제거)
- `.sanho_base.json` 생성
- `<git-common-dir>/sanho/canonical` private clone 생성과 첫 fetch
- hook 교체: v0.1 7종 라인 제거 → v0.2 6종 라인 설치
- `.gitignore` 항목 추가
- `~/.sanho/state.json`(+`.bak`)을 v2 레지스트리로 교체

### 5-2. 롤백 절차

```bash
# 1) v0.1 binary를 다시 설치한다
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.6
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.6

# 2) 작업공간 파일을 되돌린다
cd /path/to/app
mv .sanho.json.bak .sanho.json
rm -f .sanho_base.json

# 3) 레지스트리를 migrate가 보존한 v0.1 백업에서 되돌린다
cp ~/.sanho/state.json.v1.bak ~/.sanho/state.json
cp ~/.sanho/state.json.v1.bak ~/.sanho/state.json.bak

# 4) v0.1 hook을 되살린다 — v0.2 migrate가 v0.1 라인을 제거했다
sanho init --project <name> --docs-repo-url <url>   # v0.1 binary로 실행

# 5) daemon을 다시 등록·기동한다 (사용자 소유)
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/xyz.rootkernel.sanho.plist
# 또는
systemctl --user enable --now sanhod.service

# 6) 확인
sanho status
sanho state --all
```

private clone(`<git-common-dir>/sanho/canonical`)은 남겨 두어도 무해하다.
v0.1은 그 경로를 보지 않는다. 정리하고 싶으면 디렉터리째 삭제한다.

canonical 저장소 자체는 migration으로 전혀 바뀌지 않는다. 같은 저장소, 같은
선형 main이며, 앞으로 만들어질 commit의 메시지 규약만 달라진다. 따라서 롤백이
문서 데이터를 되돌리는 일은 없다.

### 5-3. migrate는 멱등이다

이미 v2인 작업공간에서 실행하면 아무것도 바꾸지 않고 성공한다.

```text
sanho: already migrated
```

legacy state에 docs 저장소 URL이 없으면 명시적으로 요구한다.

```text
sanho: the docs repository URL is not recorded in the legacy state; rerun with --docs-repo-url <url>
```

v0.1 base가 없거나 canonical에서 사라졌으면 migration을 멈추지 않고 알린 뒤
계속한다. 두 경우 모두 `sanho sync`나 `sanho doctor --fix`가 나중에 해결한다.

```text
sanho: no v0.1 docs base was found; run 'sanho sync' to establish one
sanho: the recorded docs base 67c4bbfeada3 is no longer in the canonical repository; canonical history may have been rewritten. Run 'sanho sync' to reconcile.
```

## 6. 최후 수단 — 작업공간 초기화

로컬 상태를 전부 버리고 다시 시작한다. **canonical 저장소는 전혀 건드리지
않으므로** 게시된 문서는 안전하다.

```bash
sanho clean --dry-run          # 무엇이 지워질지 확인한다. 아무것도 바꾸지 않는다
sanho clean -y                 # hook, .sanho*, private clone, 레지스트리 항목 제거
sanho init --project <name> --docs-repo-url <url>
```

`--dry-run`은 엄격하게 읽기 전용이다. 잠금 파일의 mtime조차 건드리지 않는다.
docs 디렉터리까지 지우려면 `--remove-docs`를 별도로 지정한다. 확인 없이 실행하지
않는다.

```text
sanho: 'sanho clean' removes this workspace's sanho state; rerun with -y to confirm, or 'sanho clean --dry-run' to preview
```

충돌 sync가 밀려 있으면 먼저 정리해야 한다.

```text
sanho: a conflicted sync is in progress; finish it, or run 'sanho sync --abort' first
```

## v0.1에서 사라진 복구 절차

다음 절차들은 그 원인이 v0.2에 존재하지 않으므로 폐기됐다. v0.1 문서를
참고하다가 이 이름을 만나면 위 절차로 대체한다.

| v0.1 절차 | v0.2에서의 상태 |
|---|---|
| `sanho pull-commit --continue / --abort / --recover` | 5단계 transaction이 없다. `sanho sync` + git 관용구 + `sanho sync --abort`가 대신한다. |
| `refs/sanho/recovery/<transaction-id>/` backup ref 조사 | recovery ref를 만들지 않는다. abort가 실패할 수 없기 때문이다. |
| `sanho fix` / `.sanho_pending_fix` 처리 | 명령과 상태 모두 없다. migration이 남은 파일을 거부 사유로만 읽는다. |
| main 선행 게시 실패 복구 | 애플리케이션 `main` 게시 계약이 없다. Sanho는 애플리케이션 ref를 움직이지 않는다. |
| 이미 게시된 invalid branch의 `docs-version` 복구 | trailer는 gate 입력이 아니다. 게시 판정은 base 파일과 canonical tree 비교로 한다. |
| daemon state 손상 시 기동 실패 복구 | daemon이 없다. 레지스트리 손상은 4-2절로 처리한다. |
| standalone `REBASE_HEAD` compare-and-delete | Sanho가 Git operation metadata를 검사하지 않는다. rebase는 git이 알아서 처리하고, Sanho hook은 그 사이에도 exit 0이다. |
