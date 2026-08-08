# Sanho

Sanho는 여러 애플리케이션 저장소의 `docs/` 디렉터리를 전용 docs 저장소와
동기화하고, 여러 작업공간이 동시에 문서를 갱신할 때 생기는 어긋남을 일찍
알려 주는 도구다.

제품은 실행 파일 **하나**다.

- `sanho`: 개발자와 Git hook이 사용하는 CLI

daemon, socket, HTTP API, Web UI, 브라우저 터미널, 세션 실행 기능은 없다.
설치하고, 기동하고, 감시하고, 잃어버릴 프로세스가 없다.

## 개념 모델

두 문장이면 충분하다.

- **게시는 `git push`에서 일어난다.** commit은 순수 git과 똑같이 로컬이고
  비공개다. `git push`를 실행할 때 Sanho가 `docs/`를 canonical 저장소에
  게시한다.
- **감지는 `git commit`에서 일어난다.** commit할 때 Sanho는 로컬 정보만 읽어
  "docs 기준이 뒤처졌다"고 한 줄 알려 준다. network를 열지 않고, 절대 commit을
  막지 않는다.

여기에서 나머지가 따라온다.

- 오프라인에서도 `git commit`은 언제나 성공한다. 코드 commit이든 docs
  commit이든 마찬가지다.
- Sanho는 애플리케이션 저장소에 **commit을 만들지 않는다.** `[SANHO] Update
  docs` 같은 것은 없다. `sanho sync`가 만드는 commit도 작성자는 사용자이며
  일반 commit이다.
- Sanho는 애플리케이션의 ref를 움직이지 않는다. `main`을 대신 push하지도
  않는다.
- 충돌 해소는 표준 git 관용구다. 편집 → `git add` → `git commit`. 끝났다는
  선언만 Sanho의 명령이다: `sanho sync --continue`.

각 작업공간은 자기 `docs/`가 canonical의 어느 commit에서 **파생됐는지**를
`.sanho_base.json`에 기록하고, 같은 값을 commit 메시지 trailer로도 남긴다.

```text
docs-base: 67c4bbfeada37f5dda8fb79aa43216ef062cd8df
docs-base-tree: 2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6
```

## 빠른 시작

지원 운영체제는 macOS와 Linux다. 필요한 것은 Go 1.25 이상(설치할 때만),
Git, 그리고 docs 저장소를 읽고 쓸 수 있는 인증이다. Node.js와 npm은 필요하지
않다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.1
sanho version
```

설치 위치는 `GOBIN`, 설정하지 않았다면 `$(go env GOPATH)/bin`이다. 이
디렉터리를 대화형 shell의 `PATH`에 포함해야 한다. Sanho가 설치하는 Git hook은
설치를 수행한 binary의 절대 경로를 기록하므로 GUI Git 클라이언트가 shell의
`PATH`를 재현할 필요는 없다.

checkout에서 빌드하려면 다음을 쓴다.

```bash
make cli-build     # bin/sanho
make install       # go install ./cmd/sanho
```

## 온보딩

애플리케이션 Git 저장소의 **최상위**에서 실행한다.

```bash
cd /path/to/app
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

`init`은 프로젝트를 등록하고, `.sanho.json`을 쓰고, 저장소 안에 private clone을
만들고, Git hook 6개를 설치하고, `.gitignore`에 상태 파일을 추가한다.
그다음 상황에 따라 셋 중 하나로 동작한다.

- **canonical에 내용이 있고 로컬 `docs/`가 없다**: canonical docs를 받아 index에
  올린다. commit은 사용자가 한다.
  ```text
  Canonical docs are staged. Commit them:  git commit -m 'docs: adopt canonical docs'
  ```
- **canonical이 비어 있다**: 기록할 base가 없다고 알린다. 첫 push가 canonical을
  만든다.
  ```text
  sanho: canonical repository is empty; your first push will publish docs
  ```
- **로컬 `docs/`가 이미 있다**: 저장소 이력의 trailer에서 base를 유도한다.
  사용자 파일은 건드리지 않는다. trailer가 하나도 없으면 추측하지 않고
  거절한다.

주요 옵션은 다음과 같다.

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `--project` | (필수) | 프로젝트 이름 |
| `--docs-repo-url` | (필수) | canonical docs 저장소 URL |
| `--docs-dir` | `docs` | 저장소 root 기준 docs 디렉터리 |
| `--actor-email` | `git config user.email` | canonical commit에 기록할 주소 |
| `--force` | off | 기존 docs를 canonical 내용으로 대체(파괴적, `-y` 필요) |

확인은 다음으로 한다.

```bash
sanho status
sanho doctor
```

## 일상 명령 넷

```bash
sanho status     # 지금 어디에 있는가
sanho sync       # 상류와 화해한다
sanho pull       # 로컬 편집이 없을 때 그냥 받아온다
git push         # 게시한다 — sanho push는 없다
```

### `sanho status` — 지금 어디에 있는가

```bash
sanho status              # 캐시된 canonical 기준(빠르다)
sanho status --refresh    # 먼저 fetch한 뒤 보고
sanho status --json       # 자동화용
```

```text
workspace : /path/to/app
project   : example
docs repo : git@github.com:example/example-docs.git (branch main)
base      : 67c4bbfeada3
canonical : 9a41f2cbbbbb
data      : canonical data is 3 minutes old
relation  : behind 2, ahead 0
sync      : 2 behind — 'sanho sync' will merge cleanly
```

`data` 줄은 언제나 이 정보가 얼마나 오래된 것인지 말한다. 24시간이 넘으면
갱신 방법도 함께 말한다. 이 정직함 덕분에 오프라인에서도 `status`가 동작한다.

같은 프로젝트의 다른 작업공간이 등록돼 있으면 `siblings` 표가 함께 나온다.
관계는 `same`, `ahead N`, `behind N`, `diverged`, `unknown`이다. `unknown`은
"이 clone이 그 commit을 알지 못한다"는 뜻이지 "같다"는 뜻이 아니다.

### `sanho sync` — 상류와 화해한다

commit할 때 이런 줄을 보면 실행한다.

```text
sanho: docs base is 2 commits behind — 'sanho sync' will merge cleanly
sanho: docs base is 2 commits behind — 'sanho sync' will report conflicts in docs/api.md; syncing sooner keeps them small
```

최신 상태면 아무것도 출력되지 않는다. 침묵이 정상 신호다.

```bash
sanho sync
```

`sanho sync`는 canonical을 받아 로컬 docs와 3-way 병합하고, 내 작업 **아래에**
base 갱신 commit을 깔아 준다. 그래서 이후 내 commit의 diff에는 내 변경만
남는다.

```text
sanho: synced docs to 9a41f2cbbbbb (commit 3f0d1a5c7e21)
```

만들어지는 commit은 작성자가 나인 평범한 commit이다.

```text
docs: sync to 9a41f2cbbbbb
```

사전 조건은 하나다. **`docs/` 경로가 깨끗해야 한다.** docs 밖에서 작업 중인
변경(staged든 아니든)은 그대로 유지되므로 신경 쓰지 않아도 된다.

```text
sanho: docs have uncommitted changes: commit or stash your docs changes first
```

### `sanho pull` — 그냥 받아온다

로컬 docs를 전혀 건드리지 않았을 때 canonical 내용을 그대로 받는다.

```bash
sanho pull
sanho pull --commit   # 갱신을 commit으로도 기록
```

로컬 편집이 있으면 덮어쓰지 않고 `sanho sync`를 가리킨다. 두 명령의 의도가
다르기 때문이다. `pull`은 소비만, `sync`는 화해다.

### `git push` — 게시한다

```bash
git push
```

```text
sanho: published docs 9a41f2cbbbbb (fast_forward)
```

`sanho push`라는 명령은 **없다.** 게시는 일반 `git push`의 pre-push hook에서
일어난다. Sanho가 하는 일은 다음과 같다.

1. `refs/heads/*` 업데이트만 본다. tag push와 branch 삭제는 그냥 통과한다.
2. 충돌 sync가 밀려 있으면 막는다.
3. push되는 commit의 docs에 충돌 마커가 남아 있으면 막는다.
4. canonical을 fetch하고, 내 docs tree와 canonical을 비교한다.
   - 이미 같으면 아무것도 하지 않는다(코드만 바꾼 push가 여기에 해당한다).
   - 내 base가 canonical head와 같으면 그대로 게시한다.
   - 상류가 움직였으면 3-way 병합을 시도한다. 깨끗하면 병합 결과를 게시하고
     push를 그대로 진행한다. 충돌이면 push를 거절하고 `sanho sync`를 안내한다.
5. 다른 사람이 그 사이에 먼저 게시했으면 다시 받아 처음부터 다시 계산한다.
   최대 3번 시도한다. force push는 어떤 경로에서도 하지 않는다.

push가 거절되면 **원격 ref는 하나도 바뀌지 않는다.** 안내된 명령을 실행하고
같은 `git push`를 다시 실행하면 된다.

## 충돌 해소 관용구

`sanho sync`가 충돌을 보고해도 **실패가 아니다.** 요청받은 일을 했고 마커가
worktree에 있다는 뜻이며, 종료 코드는 0이다.

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
내가 쓴 문장
=======
상류에서 온 문장
>>>>>>> sanho-upstream
```

해소는 다른 merge 충돌과 똑같다. 배울 것이 하나 있고, `git rebase --continue`와
같은 것이다: 끝났으면 끝났다고 말한다.

```bash
$EDITOR docs/api.md docs/schema.md
git add docs/
git commit -m "docs: resolve sync conflicts"
sanho sync --continue
git push
```

되돌리고 싶으면 언제든 abort할 수 있다.

```bash
sanho sync --abort
```

```text
sanho: sync aborted; docs restored to HEAD
```

**이 명령은 실패할 수 없다.** ref를 움직이지 않고, commit을 만들지 않고, docs
worktree와 상태 파일 둘만 되돌리기 때문이다. 중간에 중단되면 다시 실행하면
된다.

해소가 끝나기 전에 commit하면 막힌다. 안내는 같은 세 선택지를 반복한다.

```text
sanho: a sync is in progress — 1 files still have conflicts:
  docs/api.md
Resolve the markers, then:  git add docs/ && git commit
Then complete the sync:     sanho sync --continue
To undo this sync:          sanho sync --abort
```

push가 충돌로 거절되는 경우도 같은 흐름으로 이어진다.

```text
sanho: your docs changes conflict with upstream (base 67c4bbfeada3 → 9a41f2cbbbbb)
  docs/api.md
Run 'sanho sync', resolve, commit, then push again.
error: push rejected — no remote ref was changed
```

Sanho가 출력하는 다음 단계 명령은 **그 상태에서 실제로 성공하는 명령만**
나온다. 성공할 수 있는 명령이 없으면 명령 대신 "manual intervention required"와
진단 정보를 출력한다. 그러니 안내된 명령을 그대로 실행해도 된다.

## 그 밖의 명령

```bash
sanho state            # 등록된 프로젝트와 작업공간
sanho state --all      # 전부
sanho doctor           # 이 작업공간의 설치 상태 점검
sanho doctor --fix     # 유실된 docs base를 이력에서 복원
sanho clean --dry-run  # 무엇이 지워질지 확인(아무것도 바꾸지 않는다)
sanho clean -y         # 이 작업공간에서 Sanho 제거
sanho project add <name> --docs-repo-url <url>
sanho project delete <name>
sanho migrate          # v0.1 작업공간을 v0.2로 전환
sanho version
```

`sanho doctor`는 경고를 찾아도 종료 코드가 0이다. 문제를 찾을 때마다 실패하는
진단 명령은 문제 조사에 쓸 수 없기 때문이다.

## 설치되는 Git hook

| hook | 역할 |
|---|---|
| `pre-commit` | staged docs의 충돌 마커 검사 + 신선도 경고(로컬 전용) |
| `commit-msg` | `docs-base` / `docs-base-tree` trailer stamp |
| `pre-push` | canonical 게시 + 마커 검사 + sync 검사 |
| `post-checkout` | HEAD 이동 후 docs base 재유도 |
| `post-merge` | 같음 |
| `post-rewrite` | 같음(amend, rebase) |

`pre-push`만 fail-closed다. 나머지 다섯은 어떤 문제가 생겨도 작업을 막지
않는다. commit 경로에서 막을 수 있는 것은 충돌 마커 게이트뿐이다.

hook 설치와 제거는 **정확한 줄 일치**로만 하므로, 기존 custom hook 내용은
원문 그대로 보존된다.

repository-local `core.hooksPath` 또는 Husky 9를 쓰는 저장소는 `sanho init`이나
`sanho migrate`에 `--manage-custom-hooks`를 명시한다. 전역·저장소 외부 경로는
여러 workspace가 공유할 수 있으므로 지원하지 않는다. Husky의 생성 shim
`.husky/_`는 수정하지 않고 `.husky/*` 사용자 script만 관리한다.

## 상태 파일

```text
<app-repo>/
├── .sanho.json              작업공간 설정 (gitignore됨)
├── .sanho_base.json         base 포인터 (gitignore됨)
└── .git/
    ├── hooks/               6개 sanho 라인
    ├── sanho/sync.json      충돌 sync 중에만 존재
    └── sanho/canonical/     private bare clone

~/.sanho/
├── state.json               레지스트리(관찰용)
├── state.json.bak           백업
└── state.lock               파일 잠금
```

`~/.sanho/state.json`은 **관찰용**이다. "이 프로젝트의 다른 checkout이 어디에
있고 마지막으로 언제 무엇을 보고했는가"에 답할 뿐, 게시 정확성은 여기에 전혀
의존하지 않는다. 손상돼도 문서를 잃지 않는다.

`.sanho*` 파일과 인증 정보는 commit하지 않는다. `sanho init`이 `.gitignore`에
항목을 넣어 준다.

## AI agent 설정

Sanho로 관리하는 프로젝트의 `AGENTS.md` 또는 `CLAUDE.md`에 다음 공용 지침을
추가한다. 원문은 [README](../../README.md)에도 있다.

```markdown
## Sanho workflow

This repository uses Sanho to synchronize its `docs/` directory with the canonical docs repository.

- At the start of a task and before any authorized commit or push, run `sanho status --json`. If it fails, report the error and do not bypass Sanho.
- If the repository is not initialized, stop and ask the user for the project name and docs repository URL. Do not guess these values or initialize the workspace on your own.
- Edit `docs/` as normal workspace files. Use normal Git commands and let the installed Sanho hooks run. Sanho never authors commits and never grants permission to commit or push.
- On a `sanho: docs base is N commits behind` warning, run `sanho sync`, then continue. That is the whole protocol.
- If `sanho sync` reports conflicts, it succeeded: markers are in the worktree and the exit code is 0. Resolve them, `git add`, and `git commit` as for any merge. If the resolution is not obvious from the two sides, stop and ask the user rather than guessing.
- Never bypass Sanho with `--no-verify`, a force push used to evade a Sanho block, a `sanho push` command (it does not exist), or manual edits to `.sanho.json`, `.sanho_base.json`, `.git/sanho/`, or Sanho-owned hook lines.
- Do not run `sanho clean`, `sanho init --force`, `sanho sync --abort`, or `sanho migrate` without explicit user approval.
- When a push is rejected, read the first stderr line, run the command Sanho names, and then retry the same `git push`. Sanho only ever names a command that succeeds in the state it was printed in.
```

## 더 읽을거리

- 구조와 계약(권위 문서): [아키텍처](../architecture.md)
- 일상 흐름과 장애 대응: [운영 가이드](../operations.md)
- 복구 절차: [복구 가이드](../recovery.md)
- 설치·업그레이드·제거: [배포 규칙](../deployment.md)
- `--json` 스키마와 자동화 규범: [CLI JSON 출력](../cli-json.md)
- 릴리스 전 수동 검증: [hands-on 테스트](../hands-on-testing.md)

## 테스트

```bash
make test
```

`make test`는 `test-prepare`, `test-unit`, `test-int`, `test-e2e`를 순서대로
실행한다. `test-e2e`는 빌드된 binary로 시나리오 매트릭스와 guidance closure
스위트(Sanho가 안내하는 모든 다음 명령을, 그 안내가 출력되는 바로 그 상태에서
실제로 실행해 성공을 검증)를 돌린다. 각 단계는 따로 실행할 수 있다.
