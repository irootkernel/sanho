# CLI JSON 출력

Sanho는 자동화 도구와 AI agent가 결과를 안정적으로 읽을 수 있도록 `--json`을
제공한다.

```bash
sanho version --json
sanho status --json
sanho status --refresh --json
sanho state --json
sanho state --all --json
sanho sync --json
sanho sync --continue --json
sanho sync --abort --json
sanho pull --json
sanho doctor --json
```

`--json`은 각 명령의 로컬 플래그다. `sanho --json status`처럼 루트 옵션으로는
쓸 수 없다. `init`, `clean`, `project`, `migrate`, `hook`에는 없다.

읽기 명령 전부(`status`, `state`, `doctor`, `version`)와 화해 명령
(`sync`, `pull`)이 지원한다. `sync`와 `pull`이 포함된 것은 의도적이다. 변경
명령에 machine 출력이 없으면 agent workflow가 사람용 문장을 파싱해야 하기
때문이다.

## 공통 규칙

- 성공 결과는 stdout에 **들여쓴 JSON 객체 하나**와 마지막 개행으로 출력한다
  (2칸 들여쓰기).
- 오류가 나면 사람이 읽을 영어 문장은 **언제나 stderr**로 간다. `--json`을
  붙인 경우에 한해 stdout에는 아래의 오류 봉투 하나가 실린다. 즉 `--json`
  stdout은 언제나 JSON 문서 하나이고, 그것이 결과 문서인지 오류 봉투인지는
  `error` 키의 유무로 구별한다. `--json` 없이 실행하면 stdout은 비어 있다.
- `--verbose`(`-v`) 진단도 stderr로 나간다. stdout은 machine 채널로 유지된다.
- 배열은 값이 없어도 `[]`로 출력한다. `null`이 아니다.
- 선택 객체(`base`)는 값이 없으면 `null`이다.
- OID는 축약하지 않고 전체 값으로 출력한다. 축약(12자)은 사람용 출력 전용이다.
- 사람용 문장, 표, 진행 메시지는 JSON에 섞지 않는다.

### 오류 봉투

`--json`으로 호출한 명령(`status`, `state`, `sync`, `pull`, `doctor`,
`version`)이 실패하면 stdout에 다음 문서를 낸다. 종료 코드는 바뀌지 않고,
stderr의 안내도 그대로 나간다.

```json
{
  "error": {
    "code": "sync_required",
    "message": "local docs have changes a fast-forward cannot carry: docs have uncommitted changes"
  }
}
```

- `code`는 아래 "machine 오류 코드"의 값 중 하나다. 문자열 그대로 비교한다.
- `message`는 사람용 문장에서 `sanho: ` 접두어와 내부 패키지 태그(`appgit:`
  등)를 걷어낸 것이다. 표시용이며, 분기에 쓰지 않는다.
- 상태를 바꾸는 안내(다음에 실행할 명령)는 stderr에만 있다. 봉투는 "무엇이
  잘못됐는가"를 답하고, 안내는 사람 채널의 것이다.

### v0.1과의 차이

v0.1의 오류 코드 어휘(`docs_hash_not_found`, `pull_commit_state_failed`,
`daemon_request_failed` …)는 **없다.** 봉투의 모양은 같지만 값이 다르므로,
그 코드로 분기하던 자동화는 아래 표로 옮겨야 한다.

## 종료 코드

| 코드 | 의미 | 자동화 동작 |
|---|---|---|
| 0 | 성공 | 문서를 읽는다. |
| 1 | 사용자가 조치할 수 있는 상태 | stderr 메시지가 다음 명령을 이름짓는다. |
| 2 | Sanho 내부 결함 | 재시도하지 말고 보고한다. panic이 여기로 분류된다. |

두 가지를 특히 주의한다.

- **충돌 sync는 성공이다.** `sanho sync`가 충돌을 만들어도 종료 코드는 0이고
  `status`가 `"conflicts"`다. 결과는 종료 코드가 아니라 문서에서 읽는다.
- **`sanho doctor`는 경고를 찾아도 0이다.** 문제 유무는 `warnings` 필드로
  읽는다. 1은 검사 자체를 실행하지 못했을 때다.

## 안정 어휘

v0.2가 분기용으로 보장하는 값은 다음과 같다. 문자열 그대로 비교해도 된다.

**`sync` / `pull`의 `status`**

| 값 | 의미 |
|---|---|
| `up_to_date` | 할 일이 없었다. canonical이 비어 있는 경우도 포함한다. |
| `synced` | docs가 화해됐고 base가 전진했다. `commit`이 비어 있을 수 있다. |
| `conflicts` | 마커가 worktree에 있고 해소가 밀려 있다. |
| `completed` | `sync --continue`가 sync를 끝냈다. `base`가 채택된 값이고 `commit`은 빈 문자열이다(완료는 commit을 만들지 않는다). |
| `aborted` | `sync --abort`가 완료됐다. |

**게시 case** — 사람용 출력 `sanho: published docs <oid12> (<case>)`에 그대로
나타난다.

| 값 | 의미 |
|---|---|
| `up_to_date` | tip의 docs가 이미 canonical과 같다. 게시하지 않았다. |
| `fast_forward` | base == canonical head. tip tree를 그대로 게시했다. |
| `auto_merge` | 상류가 움직였고 자동 병합이 clean이어서 병합 결과를 게시했다. |
| `unknown_base` | base가 canonical 게시 branch에서 도달 불가. 재유도 대상. |

**push 거절 사유** — `sanho: docs must be reconciled before publishing
(<reason>; base <a> → <b>)` 안에 그대로 나타난다.

| 값 | 의미 |
|---|---|
| `conflicts` | 3-way 병합이 충돌했다. |
| `no_base` | 기록된 base가 없어 병합 base를 정할 수 없다. |
| `cas_retry_exhausted` | 다른 게시자가 3회 연속 경합에서 이겼다. |

**machine 오류 코드** — `--json` 오류 봉투의 `error.code`.

| 값 | 언제 |
|---|---|
| `not_in_workspace` | 현재 디렉터리가 관리 대상 작업공간이 아니다. |
| `v1_workspace` | v0.1 layout이다. `sanho migrate`만 성공한다. |
| `sync_in_progress` | 충돌 sync가 미해소 상태다(없어야 할 때 없는 경우도 포함). |
| `sync_required` | 화해가 필요하다. dirty pull, 충돌 push, docs를 지우는 게시가 여기다. |
| `docs_dirty` | docs에 commit되지 않은 변경이 있어 sync를 시작할 수 없다. |
| `history_rewritten` | 기록된 base가 canonical 이력에 없다. |
| `unknown_target` | `--rebase-onto` 대상이 canonical commit이 아니거나, 건강한 base의 조상이다. |
| `canonical_unreachable` | canonical에 닿지 못했거나 병합을 수행하지 못했다. |
| `registry_lock_timeout` | 다른 Sanho 프로세스가 registry 잠금을 쥐고 있다. |
| `markers_present` | push되는 docs에 충돌 마커가 있다. `sync --continue`가 마커를 이유로 거절할 때도 이 코드다. |
| `too_large` | 텍스트 문서가 마커 스캔 한계를 넘었다. |
| `config_corrupt` | `.sanho.json`이 있으나 읽을 수 없다. 복원하거나 re-init한다. |
| `base_corrupt` | base 파일이 있으나 읽을 수 없다. 복원하거나 `doctor --fix`로 재유도한다. |
| `base_not_corroborated` | base 기록을 뒷받침할 증거가 없어 쓰기가 거절됐다. `sanho sync`가 base를 세운다. |
| `internal` | 위 어디에도 속하지 않는다. 재시도하지 말고 보고한다. |

**sibling 관계** — `vs_mine`, `vs_head`.

| 값 | 의미 |
|---|---|
| `same` | 같은 commit. |
| `ahead N` | 대상 쪽에만 N개 commit이 있다. |
| `behind N` | 기준 쪽에만 N개 commit이 있다. |
| `diverged` | 양쪽에 서로 다른 commit이 있다. |
| `unknown` | 이 clone이 두 commit을 배치할 수 없다. 추측하지 않는다. |

**doctor severity**: `ok`, `info`, `warning` 세 값이다. `warnings` 필드는
`warning`만 센다. `info`는 문제의 등급이 아니라 "할 말은 있고 요구할 것은
없는" 검사를 뜻한다 — 지금은 base 재유도가 §5.10에 따라 의도적으로 보류된
상태를 보고하는 데 쓴다.

**doctor 검사 이름**: `git`, `workspace-config`, `hooks`, `hooks-fix`,
`clone`, `canonical-head`, `origin`, `base`, `base-derivation`, `base-fix`,
`registry`, `sync`, `docs`. `base-fix`와 `hooks-fix`는 `--fix`로 복구를
시도했을 때만 나타나고, `base-derivation`은 기록된 base와 이력에서 재유도한
base가 어긋났을 때만 나타난다.
`origin`은 canonical 저장소에 실제로 닿아 보는 검사이며 경고만 낸다 — 모든
읽기 경로는 마지막 fetch로 동작하므로, 닿지 않는다는 사실은 보고할 가치가
있을 뿐 진단 명령을 실패시킬 이유가 아니다.

**고정 메시지 접두어** — 자동화가 상태를 식별해야 할 때 쓸 수 있는, 코드의
메시지 카탈로그가 고정하는 문자열이다.

```text
sanho: this workspace uses the v0.1 layout; run 'sanho migrate'
sanho: not a sanho workspace (no .sanho.json here); run 'sanho init' to create one
sanho: finish the sync first: resolve the conflicts, 'git add' and 'git commit', then 'sanho sync --continue' (or 'sanho sync --abort' to undo it)
sanho: docs base is N commits behind — …
sanho: merged docs with upstream — N files have conflicts:
sanho: your docs changes conflict with upstream (base <a> → <b>)
sanho: docs must be reconciled before publishing (<reason>; base <a> → <b>)
sanho: pushed docs still contain conflict markers:
sanho: canonical repository unreachable (<url>): <원인>
sanho: canonical history was rewritten; base <oid> is no longer reachable
sanho: branch <name> carries no docs; publishing it would delete …
sanho: the sync from <a> to <b> is not completed; no commit has changed the files it conflicted on
sanho: this sync cannot be completed here (<원인>)
sanho: this branch carries no docs provenance, so the docs base was cleared — run 'sanho sync' to establish one
sanho: the record of the sync in progress is unreadable (<원인>)
error: push rejected — no remote ref was changed
```

`SANHO_ALLOW_DOCS_DELETION=1`을 앞에 붙여 push하면 `branch <name> carries no
docs` 거절을 그 한 번만 무력화한다. docs가 없는 branch를 게시해 canonical의
모든 문서를 삭제하는 일은 정당한 작업이지만, branch에 docs가 없다는 사실만으로
추론할 수 있는 의도가 아니어서 명시를 요구한다.

값은 프로세스 환경에서 읽으므로 `export`로 두면 그 셸의 이후 push 전부에
적용된다 — "한 번만"이 아니다. 그래서 안내 문구는 실제로 한 번만 유효한
`SANHO_ALLOW_DOCS_DELETION=1 git push` 접두 형태를 보여 준다.

## `version`

```json
{
  "name": "sanho",
  "version": "v0.2.1"
}
```

commit과 build date는 사람용 `sanho version` 출력에만 있다. 기계가 분기할
식별자가 아니기 때문이다. 이 스키마는 v0.1과 동일하므로 기존 스크립트가 그대로
동작한다.

## `status`

현재 작업공간과 canonical의 관계를 반환한다. 기본은 마지막 fetch 결과를 쓰고,
`--refresh`를 주면 먼저 fetch한다.

```json
{
  "project": "product",
  "workspace_id": "product:/Users/name/work/app",
  "base": {
    "commit": "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
    "tree": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6"
  },
  "canonical": {
    "head": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "tree": "aa11bb22cc33dd44ee55ff6677889900aabbccdd",
    "empty": false,
    "fetched_ever": true,
    "data_age_seconds": 187,
    "publication_url": "git@github.com:example/example-docs.git",
    "publication_branch": "main"
  },
  "relation": {
    "known": true,
    "behind": 2,
    "ahead": 0
  },
  "publication": {
    "known": true,
    "pending": false
  },
  "sync_preview": {
    "known": true,
    "clean": false,
    "conflicts": ["docs/api.md"]
  },
  "sync_in_progress": false,
  "siblings": [
    {
      "workspace_id": "product:/Users/name/work/other",
      "base_commit": "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
      "base_tree": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
      "vs_mine": "same",
      "vs_head": "behind 2",
      "actor_email": "dev@example.com",
      "last_updated_at": "2026-08-07T09:14:03Z"
    }
  ]
}
```

필드 의미는 다음과 같다.

| 필드 | 설명 |
|---|---|
| `base` | 기록된 base. 없으면 `null`이다(빈 canonical에 대한 fresh init). `tree`는 legacy `docs-version`에서 채택한 경우 빈 문자열일 수 있다. |
| `canonical.head` / `.tree` | 마지막 fetch 시점의 canonical head와 그 docs tree. `empty`가 `true`면 둘 다 빈 문자열이다. |
| `canonical.empty` | canonical 게시 branch에 commit이 하나도 없다. 첫 push가 부트스트랩한다. |
| `canonical.fetched_ever` | clone이 한 번이라도 fetch에 성공했는지. `false`면 `data_age_seconds`는 0이다. |
| `canonical.data_age_seconds` | 마지막 성공 fetch로부터 지난 초. 사람용 출력은 24시간을 넘기면 갱신 방법을 함께 말한다. |
| `canonical.publication_url` | 작업공간 설정의 docs 저장소 URL. |
| `canonical.publication_branch` | 해석된 게시 branch. `main`, 없으면 `master`. |
| `relation.known` | base가 이 clone에서 해석되고 거리 계산이 성공했을 때만 `true`. `false`면 `behind`/`ahead`는 0이며 **모른다는 뜻이지 같다는 뜻이 아니다.** |
| `relation.behind` | canonical이 base보다 앞서 있는 commit 수. |
| `relation.ahead` | base가 canonical head보다 앞서 있는 commit 수. |
| `publication.known` | base가 있고 sync가 진행 중이지 않으며 local HEAD docs tree를 읽었을 때만 `true`. |
| `publication.pending` | `known=true`일 때 local HEAD docs tree가 `base.tree`와 다르다. hook 장애 중 app push만 완료된 상태도 탐지한다. |
| `sync_preview.known` | 예측을 계산했는지. `false`면 `clean`과 `conflicts`를 신뢰하지 않는다. |
| `sync_preview.clean` | `sanho sync`가 충돌 없이 병합될 것으로 예측된다. |
| `sync_preview.conflicts` | 충돌이 예상되는 경로. 저장소 기준 상대 경로다(`docs/api.md`). |
| `sync_in_progress` | `sync.json`이 존재한다. `true`면 push가 막힌다. |
| `siblings` | 같은 프로젝트의 **다른** 등록 작업공간. 자기 자신은 포함하지 않는다. `workspace_id` 오름차순이다. |
| `siblings[].last_updated_at` | 그 작업공간이 마지막으로 자기 상태를 보고한 시각(RFC3339, UTC). 보고한 적이 없으면 `0001-01-01T00:00:00Z`. |

`relation`과 `sync_preview`는 canonical이 비어 있거나 base가 clone에서 해석되지
않으면 계산하지 않는다. `publication`은 canonical 관계와 독립적인 local 축이며,
base가 없거나 sync 진행 중이거나 HEAD tree를 읽지 못하면 `known=false`다. 값을
지어내지 않는 것이 규칙이다.

sibling 항목은 다른 checkout이 **보고한 값**이다. 이 clone이 가지고 있지 않은
commit을 가리킬 수 있으며, 그때 관계는 `unknown`이다.

## `state`

레지스트리(`~/.sanho/state.json`)를 덤프한다. 작업공간 안에서는 그 작업공간의
프로젝트로 범위를 좁히고, `--all`이면 전부 출력한다. 작업공간 밖에서도 동작하며
이때 `scope`는 `all`이다.

```json
{
  "home": "/Users/name/.sanho",
  "scope": "product",
  "projects": [
    {
      "name": "product",
      "docs_repo_url": "git@github.com:example/example-docs.git",
      "head": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f"
    }
  ],
  "workspaces": [
    {
      "workspace_id": "product:/Users/name/work/app",
      "project": "product",
      "local_path": "/Users/name/work/app",
      "base_commit": "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
      "base_tree": "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
      "actor_email": "dev@example.com",
      "last_updated_at": "2026-08-07T09:14:03Z"
    }
  ]
}
```

| 필드 | 설명 |
|---|---|
| `home` | 해석된 sanho home. `SANHO_HOME`이 있으면 그것, 없으면 `~/.sanho`. |
| `scope` | 프로젝트 이름 또는 `"all"`. |
| `projects[].head` | **선택 필드다.** 그 프로젝트의 작업공간 안에서 실행했을 때만 나타난다. canonical head는 작업공간별 private clone에만 있으므로 다른 곳에서는 읽을 데가 없다. 없으면 키 자체가 생략된다. |
| `workspaces` | `workspace_id` 오름차순. 범위 밖 프로젝트의 항목은 제외된다. |

두 배열은 이름/ID 오름차순으로 정렬되므로 출력이 안정적이다.

## `sync` / `pull`

두 명령이 같은 스키마를 쓴다.

```json
{
  "status": "conflicts",
  "base": {
    "commit": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "tree": "aa11bb22cc33dd44ee55ff6677889900aabbccdd"
  },
  "commit": "",
  "conflicts": ["docs/api.md", "docs/schema.md"]
}
```

| 필드 | 설명 |
|---|---|
| `status` | 위 "안정 어휘"의 다섯 값 중 하나. |
| `base` | 적용된 새 base(canonical head 또는 `--rebase-onto` 대상). `up_to_date`에서도 현재 base를 그대로 반복한다. canonical이 비어 있으면 이름 붙일 commit이 없으므로 `null`이다. `--abort`에서도 `null`이다. **`conflicts`에서는 아직 채택되지 않은 병합 대상**을 가리킨다 — 충돌 sync는 base 파일을 옮기지 않고 sync note에 대상을 기록해 두었다가 `sanho sync --continue`가 채택한다. `completed`에서는 방금 채택된 값이다. |
| `commit` | 만들어진 sync commit OID. 아무것도 commit하지 않았으면 빈 문자열이다. 병합 결과가 `HEAD`의 docs tree와 같으면 기록할 변경이 없으므로 `synced`이면서 `commit`이 비어 있을 수 있다. |
| `conflicts` | 충돌 경로(저장소 기준 상대 경로). `conflicts` 상태가 아니면 `[]`. |

`sync --continue --json`은 채택된 base와 함께 완료를 보고한다.

```json
{
  "status": "completed",
  "base": {
    "commit": "9a41f2cbf0d1e2a3b4c5d6e7f8091a2b3c4d5e6f",
    "tree": "aa11bb22cc33dd44ee55ff6677889900aabbccdd"
  },
  "commit": "",
  "conflicts": []
}
```

끝낼 수 없는 상태에서는 §5.8 오류 봉투로 거절하며, 코드가 무엇이 남았는지
구분한다. 마커가 남아 있으면 `markers_present`, 해소를 commit하지 않았으면
`docs_dirty`, note가 없으면 `sync_in_progress`다.

`sync --abort --json`은 다음을 출력한다.

```json
{
  "status": "aborted",
  "base": null,
  "commit": "",
  "conflicts": []
}
```

## `doctor`

```json
{
  "workspace": "/Users/name/work/app",
  "checks": [
    {
      "name": "git",
      "severity": "ok",
      "detail": "git version 2.44.0 (no minimum is enforced; merges need git 2.38 or newer)"
    },
    {
      "name": "workspace-config",
      "severity": "ok",
      "detail": "schema version 2, docs dir docs, project product"
    },
    {
      "name": "hooks",
      "severity": "ok",
      "detail": "all 6 hooks installed exactly once"
    },
    {
      "name": "clone",
      "severity": "ok",
      "detail": "/Users/name/work/app/.git/sanho/canonical, branch main, fetched 3 minutes ago"
    },
    {
      "name": "base",
      "severity": "ok",
      "detail": "commit 67c4bbfeada3, tree 2f41ab90c3d2"
    },
    {
      "name": "registry",
      "severity": "ok",
      "detail": "/Users/name/.sanho is readable and lockable"
    },
    {
      "name": "sync",
      "severity": "ok",
      "detail": "no sync is in progress"
    },
    {
      "name": "docs",
      "severity": "ok",
      "detail": "42 files, 183204 bytes, 0 symlinks in docs"
    }
  ],
  "warnings": 0
}
```

`checks`의 순서는 고정이며(`git` → `workspace-config` → `hooks` → `clone`
[→ `canonical-head`] → `base` [→ `base-derivation`] [→ `base-fix`] [→ `publication`] →
`registry` → `sync` → `docs`),
`warnings`는 `severity == "warning"`인 항목 수다. `detail`은 사람이 읽는
문장이므로 **파싱 대상이 아니다.** 분기는 `name`과 `severity`로 한다.

## 에이전트 자동화 규범

### 다음 명령은 검증된 안내다

Sanho는 다음 원칙을 강제한다. **출력된 모든 다음 단계 명령은, 그것이 출력된
상태에서 실제로 성공한다.** 성공할 수 있는 명령이 없으면 명령 대신
`manual intervention required`와 진단 정보를 출력한다.

이 원칙은 설계 문구가 아니라 구현 구조로 보장된다.

- 다음 단계 명령을 담은 모든 문자열은 CLI의 메시지 카탈로그 한 곳에 모여
  있고, 호출 지점에서 조립하지 않는다. 같은 파일이 각 안내를
  `{ID, Source, Scenario, Sample, Match, NextCommands}` 항목으로 열거하는
  카탈로그를 노출한다.
- 패키지 단위 테스트가 메시지 파일을 **소스로 파싱해** 다음 단계 명령을 담은
  리터럴을 찾아내고, 카탈로그에 등록되지 않은 안내가 있으면 빌드를 실패시킨다.
  등록을 빠뜨린 채 안내를 추가할 수 없다.
- `test/cli/e2e`의 closure 스위트가 카탈로그 항목마다 실제 작업공간을 그
  상태로 만들고, 메시지가 실제로 출력되는지 확인한 다음, 안내된 명령을 그
  상태에서 그대로 실행해 성공을 요구한다. 명령마다 새 작업공간을 쓴다.
- `sanho sync --abort`는 ref를 움직이지 않고 commit을 만들지 않으므로 실패할
  수 있는 상태가 없다.
- 게시가 `no_base`로 거절할 때 `sanho sync`가 실제로 성공하도록, base 없는
  sync는 empty tree를 병합 base로 삼는다.
- rewrite 안내는 `docs-base-tree` 검색을 다시 수행해 실재하는 commit만 이름에
  넣고, 조회 명령은 `origin/HEAD`가 아니라 실제 게시 branch를 지목한다.
- 통합 테스트가 push 거절 → `sanho sync` → 해소 → commit → push 성공 경로를
  실제 git 저장소에서 끝까지 실행해 이 폐쇄성을 다시 확인한다.

따라서 agent는 안내된 명령을 그대로 실행해도 좋다. 다만 `sanho clean`,
`sanho init --force`처럼 파괴적인 명령과, 사용자 의도가 필요한 선택(해소 대
abort)은 사용자 확인 없이 고르지 않는다.

### 권장 흐름

```bash
sanho status --json          # 작업 시작 시, commit/push 전
```

1. `sync_in_progress`가 `true`면 **끝내기**(해소 → `git add` → `git commit` →
   `sanho sync --continue`) 또는 **되돌리기**(`sanho sync --abort`) 중 하나를
   사용자와 확인하고 진행한다. commit만으로는 끝나지 않으며, 끝날 때까지 push는
   거절된다.
2. `relation.known && relation.behind > 0`이면 `sanho sync --json`을 실행한다.
   - `status == "synced"` → 계속 진행한다.
   - `status == "conflicts"` → `conflicts` 목록을 사용자에게 제시한다. 마커를
     해소하고 `git add` + `git commit` 뒤 `sanho sync --continue`로 끝낸다.
     `--continue`가 `completed`를 돌려주기 전까지 sync는 끝나지 않았다.
   - `status == "up_to_date"` → 할 일이 없다.
3. commit과 push는 **일반 git 명령**으로 한다. Sanho에는 `sanho push`가 없고,
   commit을 만드는 명령도 없다.
4. push가 거절되면 stderr의 첫 줄로 원인을 판별하고 안내된 명령을 실행한 뒤
   **같은 `git push`를 다시 실행**한다.

### 금지 사항

- `--no-verify`, Sanho 차단을 피하기 위한 force push, hook 파일 수동 편집.
- `.sanho.json`, `.sanho_base.json`, `.git/sanho/` 직접 수정·삭제.
- 사람용 표 출력 파싱. `--json`을 쓴다.
- 종료 코드만으로 sync 결과 판정. 충돌 sync는 0이다.
- commit이 sync를 끝낸다는 가정. `sanho sync --continue`가 `completed`를
  돌려주는 것이 완료의 유일한 신호다.
- `detail` 문장 파싱. `name`과 `severity`로 분기한다.
- `relation.known == false`를 "차이 없음"으로 해석. 모른다는 뜻이다.

관련 문서: [아키텍처](architecture.md), [운영 가이드](operations.md),
[복구 가이드](recovery.md).
