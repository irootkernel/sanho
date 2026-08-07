# Sanho 아키텍처

이 문서는 v0.2 구현의 권위 문서다. 런타임, Git, 동기화, 영속화, 동시성,
안전 계약을 여기에서 정의한다. 설계 기록인 `sanho-v0.2.md`는 v0.2가
구현되면서 역사 문서가 되었고, 구현과 설명이 어긋나면 코드와 이 문서를
기준으로 판단한다.

## 제품 경계

Sanho의 책임은 전용 docs 저장소를 단일 진실의 원천으로 유지하고,
애플리케이션 저장소의 `docs/` 작업 사본이 어느 canonical commit에서
파생됐는지 추적하는 것이다.

실행 구성요소는 `sanho` CLI **하나**다. daemon, Unix socket, HTTP API,
Web UI, 브라우저 터미널, 세션 실행 기능은 존재하지 않는다. Sanho는 사용자
대신 셸 명령을 실행하지 않고, 애플리케이션 저장소에 commit을 만들지
않는다.

동작에 필요한 것은 설치된 `git`과 docs 저장소에 접근할 수 있는 자격 증명뿐이다.
상주 프로세스가 없으므로 CI, container, SSH 세션에서도 동일하게 동작한다.

## 네 가지 원칙

모든 설계 판단은 이 네 원칙에서 유도한다. 판단이 갈릴 때는 원칙 쪽을
선택한다.

| # | 원칙 | 결과 |
|---|---|---|
| P1 | **push에서 게시한다.** canonical 게시는 `git push` 시점에만 일어난다. | commit은 순수 git과 똑같이 로컬·비공개다. |
| P2 | **commit에서 감지한다.** commit 경로는 읽기 전용이고 network를 열지 않으며 절대 막지 않는다. | `git commit`은 오프라인에서도 항상 성공한다. |
| P3 | **도구는 애플리케이션 저장소에 commit을 만들지 않는다.** 모든 commit은 사용자가 일반 git 명령으로 만든다. | `[SANHO]` commit도, ref 조작도, transaction engine도 없다. |
| P4 | **daemon이 없다.** 조정은 git 자신의 push 의미론으로, 로컬 공유 상태는 파일 잠금으로 처리한다. | 설치·기동·감시·유실할 프로세스가 없다. |

## 구성

```text
application repository
  working tree                      .git/
    src/…                             hooks/                6개 sanho 라인
    docs/            <-- pull/sync    sanho/sync.json        충돌 sync 중에만
    .sanho.json      workspace 설정   (git-common-dir)/sanho/canonical/
    .sanho_base.json base 포인터                             private bare clone
  commit trailer: docs-base / docs-base-tree

        |  ^                                     |
        |  |  git fetch (local transport,        |  git fetch origin
        |  |  객체가 양방향으로 오간다)             v
        |  +------------------------------ private bare clone
        |                                        |
        |                                        |  git push
        |                                        |  CAS = --force-with-lease
        v                                        v
  사용자의 편집                            canonical docs repository
                                          (origin, 선형 main history)

~/.sanho/                       flock으로 보호되는 관찰용 레지스트리
  state.json                    프로젝트 -> URL, 작업공간 상태
  state.json.bak                동일 내용 백업
  state.lock                    배타 잠금 대상

sanho CLI (단일 binary)
  명령: init · status · state · sync · pull · clean · doctor · project ·
        hook · migrate · version
  hook: pre-commit · commit-msg · pre-push ·
        post-checkout · post-merge · post-rewrite
```

**canonical 저장소는 docs 전용이다.** canonical commit의 **root tree가 곧
docs tree**이고, `docs/`라는 하위 디렉터리를 두지 않는다. 이 등식은 장식이
아니라 계약이다. 게시가 애플리케이션의 `docs/` subtree를 그대로 root tree로
commit하고, 소비가 canonical commit의 root tree를 그대로 `docs/`에 펼치며,
재유도(`docs-base-tree`)가 두 세계의 tree OID를 직접 비교하는 것이 전부 이
등식 위에 서 있다. 하위 디렉터리를 가진 저장소를 canonical로 지정하면 첫
`sanho pull`이 그 구조를 `docs/` 아래에 그대로 펼친다 — 조용한 오작동이 아니라
눈에 보이는 결과이며, 그것이 잘못된 저장소를 지목했다는 신호다. Sanho는
init 시점에 이를 검사하지 않는다. "docs 전용"은 내용에 대한 판단이지 구조에
대한 판단이 아니어서, 기계가 확인할 수 있는 형태가 아니기 때문이다(F-L11에서
의도적으로 좁힌 범위).

데이터 흐름은 세 가지다.

- **게시**(pre-push): 애플리케이션 tip의 docs tree → private clone(local
  fetch) → canonical 게시 branch 위 commit → origin push.
- **소비**(`sanho pull` / `sanho sync`): origin fetch → private clone →
  애플리케이션 저장소로 object import → `docs/` 반영 → base 파일 갱신.
- **관찰**(`sanho status` / `state`): base 파일 + private clone(관계 계산)
  + `~/.sanho/state.json`(sibling 목록).

## 패키지 배치

계층 규칙은 `internal/architecture`의 테스트가 강제한다. usecase는 infra를
import하지 않고, infra는 usecase를 import하지 않는다. 두 계층을 함께 보는
곳은 `internal/interface/cli` 하나뿐이며, 어댑터 배선도 거기에서 한다.

| 경로 | 책임 |
|---|---|
| `cmd/sanho` | 단일 실행 파일 진입점. ldflags 빌드 정보를 CLI에 전달한다. |
| `internal/buildinfo` | 주입된 버전과 module 버전을 해석한다. |
| `internal/domain/provenance` | `Base`, trailer 키, stamp 규칙, 이력 기반 base 선택. 순수 로직. |
| `internal/domain/publish` | 게시 case 판정(`Decide`), canonical commit 메시지 형식, 전송 sentinel. |
| `internal/domain/markers` | 충돌 마커 탐지. 바이트 위에서만 동작한다. |
| `internal/usecase/publish` | pre-push 게시 흐름: gate → fetch → case 분석 → CAS push → base 전진. |
| `internal/usecase/docsync` | `sync`, `sync --abort`, `sync --rebase-onto`, `pull`, 해소 감지. |
| `internal/usecase/admin` | `status` 계산(관계, sync 예측, sibling 관계). |
| `internal/infra/gitx` | 단일 git 실행기. argv 전용, 환경·timeout 정책, exit code 분류. |
| `internal/infra/fsx` | 원자적 파일 쓰기와 `flock`. 모든 상태 저장이 여기를 거친다. |
| `internal/infra/appgit` | 애플리케이션 저장소 어댑터(docs tree 읽기/쓰기, 마커 스캔, hook 설치·제거). |
| `internal/infra/canonical` | private clone 관리, `merge-tree` 병합, commit-tree, CAS push, `Link` 결합. |
| `internal/infra/wsstate` | `.sanho.json`, `.sanho_base.json`, `sync.json`. |
| `internal/infra/registry` | `~/.sanho/state.json`, `.bak`, `state.lock`. |
| `internal/interface/cli` | 명령·hook 표면, 메시지 카탈로그, 포트 배선, lifecycle glue(init/clean/migrate/doctor). |
| `internal/architecture` | 계층 규칙 guardrail 테스트. |

`init`, `clean`, `migrate`, `doctor`는 usecase가 아니라 `interface/cli`에
있다. 이들은 검증할 불변식이 아니라 순서가 있는 파일·git 효과이고, 포트로
감싸면 구현체와 호출자가 각각 하나뿐인 인터페이스만 늘어나기 때문이다.

## provenance 계약

### trailer 두 종과 legacy 공존

`commit-msg` hook이 base 파일 값을 그대로 복사해 다음 trailer를 붙인다.

```text
docs-base: <canonical commit OID>
docs-base-tree: <그 commit의 docs tree OID>
```

값은 `^(?:[0-9a-f]{40}|[0-9a-f]{64})$`에 맞는 소문자 전체 OID다. tree 값이
없으면 `docs-base-tree` 줄은 생략한다. stamp는 순수 로컬 동작이다. network,
clone 접근, 원격 조회를 전혀 하지 않는다.

출처는 **언제나 base 파일**이다. 예외는 없고, 특히 sync 창 안에도 없다
(§창 안 stamping은 언제나 base 파일 값이다).

의미는 **파생(ancestry)**이다. `docs-base: X`는 "이 commit의 docs 내용은
canonical commit X에서 파생됐다"를 뜻하며, tree가 같다는 주장(identity)이
아니다. "이 tip이 게시됐는가"는 어디에도 저장하지 않고 필요할 때
`tip docs tree == canonical HEAD tree`로 계산한다.

v0.1의 `docs-version:` trailer는 identity 의미였다. `docs-version: X`인
commit의 docs tree는 당시 canonical X와 같았으므로, X는 그 위에서 만든
편집의 올바른 *base*이기도 하다. 따라서 이력을 스캔하는 모든 경로가 두 키를
모두 읽고, 혼합 이력에는 rewrite도 migration commit도 필요 없다.

한 commit에서 `docs-base`가 존재하면 그 값만 본다. 값이 정확히 하나이고
형식이 맞아야 채택하며, 그렇지 않으면 그 commit 전체를 실격 처리하고
`docs-version`으로 대체하지 않는다. `docs-base`가 아예 없을 때만
`docs-version`을 본다.

### stamp 규칙

메시지에 이미 `docs-base:` 줄이 있으면 stamp하지 않는다. **줄 맨 앞에서**
시작하는 것만 센다. git 자신의 trailer 규칙이 그렇고, Sanho가 쓰는 형식도
그렇다. 공백을 걷어내고 비교하면 들여쓰인 `docs-base:`도 세게 되는데, squash
merge의 본문은 정확히 그런 줄로 가득하다(git이 squash된 각 메시지를
들여쓴다). 그러면 squash commit은 자기 stamp를 억제하면서 정작 아무것도
파싱하지 못해, trailer도 없고 재유도도 안 되는 commit이 남는다. 들여쓰인 줄은
억제하지도 않고 읽히지도 않는다 — 두 쪽이 같은 규칙을 쓰므로 일관된다.
`commit.template`으로 들여쓴 trailer 예시를 넣어 두는 경우도 같은 이유로
무해하다.

그 외에는 다음 중 하나라도 참이면 stamp한다.

1. index의 docs tree가 `HEAD`의 docs tree와 다르다(docs를 바꾸는 commit).
2. `HEAD`의 docs tree가 `HEAD~`의 docs tree와 다르다(docs를 건드린 commit의
   `--amend`, 메시지만 바꾸는 reword로 trailer가 지워진 경우 포함).

`HEAD~`를 해석할 수 없으면 empty tree로 취급한다. 이 규칙은 docs commit
직후의 첫 non-docs commit에 한해 과잉 stamp를 한다. trailer는 기록이자
복구원이지 gate 입력이 아니므로 정확한 trailer가 하나 더 붙는 것은 무해하다.

base 파일이 없거나 형식이 깨졌으면 `commit-msg`는 한 줄 경고만 출력하고
commit을 통과시킨다.

```text
sanho: docs provenance not stamped (<원인>); run 'sanho doctor --fix' to restore it
```

### base 재유도

`post-checkout`, `post-merge`, `post-rewrite`는 같은 본문을 실행한다. base는
"체크아웃된 내용"의 속성이므로 HEAD가 움직인 뒤 로컬에서 다시 계산한다.

0. **sync note가 있으면 아무것도 하지 않는다.** 진행 중인 sync가 base를
   소유한다. 그 sync는 base를 직전 값에 붙들어 두고 `--continue`가 대상을
   채택하므로, 창 안의 재유도는 sync가 붙들고 있는 파일을 제3자가 쓰는 일이
   된다. 물러나도 잃는 것은 없다. note는 어떤 체크아웃에서도 살아남는다.

   **이 가드는 더 이상 안전의 최후 방어선이 아니다.** 창 안의 모든 commit이
   base 파일 값을 새기므로(§창 안 stamping), 이력에 대상을 담은 trailer 자체가
   생기지 않는다. 가드가 뚫려도 채택되는 값은 기껏해야 너무 오래된 base다.
   가드는 창 안에서 base가 흔들리는 잡음을 막기 위해 유지한다.
1. worktree docs tree와 `HEAD`의 docs tree가 다르면 아무것도 하지 않는다.
   체크아웃을 넘어 살아남은 미commit 편집이 있다는 뜻이고, base는
   "worktree docs가 어디에서 파생됐는가"에 답해야 하기 때문이다.
2. `git log --max-count=500 HEAD`를 걸어 각 commit의 trailer를 모으고,
   최신순으로 처음 채택 가능한 commit의 base를 고른다.
3. **채택할 것이 하나도 없고**, 기록된 base의 docs tree가 worktree docs와
   같지도 않으면 **base를 지운다.**
4. 채택한 commit이 현재 기록된 base commit과 같으면 파일을 그대로 둔다.
   legacy `docs-version` 채택은 tree가 없으므로, 덮어쓰면 rewrite 복구
   anchor를 잃는다.
5. 기록된 base의 docs tree가 worktree docs 그 자체이면 역시 그대로 둔다.
   문서에 대해 다툴 것이 없는데 포인터만 다시 쓰는 것은 잡음이고, 방금
   게시하거나 `pull`한 workspace가 모두 이 상태다 — 거기서 재유도하면 다음
   HEAD 이동에서 base가 뒤로 끌려가고 그 다음 push가 아무도 바꾸지 않은
   내용을 다시 병합한다.
6. 실제로 바뀐 경우에만 한 줄 출력한다.

```text
sanho: docs base re-derived as <oid12> after HEAD moved
```

세 hook 모두 어떤 실패에서도 exit 0이다. `post-checkout`은 git이 넘기는 세
번째 인자를 읽어 **파일 체크아웃(flag 0)이면 물러난다** — `git checkout --
docs/api.md`는 ref를 움직이지 않으므로 재유도할 이력 변화 자체가 없고, 문서
하나를 되돌리는 그 동작은 1번이 이미 물러나야 하는 상태다.

#### 3번 — 근거 없는 base는 지운다

이것이 4차 리뷰의 C2다. 예전에는 "채택할 것이 없다"를 조용히 "그대로 둔다"로
읽었다. 하지만 base 파일은 checkout 루트에 있는 **파일 하나**이고 움직인 것은
`HEAD`다. "그대로 둔다"가 실제로 한 일은 **지금 거기 선 branch에게 다른
branch의 base를 넘겨주는 것**이었다.

sanho 도입 전에 이미 `docs/`가 있던 저장소는 그런 branch를 구조적으로 가진다.
하나를 체크아웃하면 workspace는 낡은 문서 하나 위에 base == canonical head를
들고 있었고, push는 fast-forward로 평가되어 canonical의 문서 여섯 개가 그 하나로
바뀌었다 — exit 0, `(fast_forward)`.

기준은 일부러 낮다. 기록된 base의 docs가 **곧 worktree의 docs**이거나 이력이
그것을 이름지으면 유지한다. 목적은 잘 돌아가는 workspace를 의심하는 것이 아니라,
검증되지 않은 포인터를 branch 전환 너머로 나르기를 거부하는 것이다. base가
없는 상태는 아래가 모두 다룬다: 게시는 `no_base`로 거절하며 `sanho sync`를
이름짓고, sync는 빈 tree(양쪽의 합집합)에 대해 병합하여 하나를 세운다.

```text
sanho: this branch carries no docs provenance, so the docs base was cleared — run 'sanho sync' to establish one
```

1번 때문에 남는 불일치는 `sanho doctor`의 `base-derivation` 검사가 보고한다.
이력에서 재유도한 base 후보와 기록된 base가 다를 때, 셋으로 갈린다. (0번의
경우는 아예 검사하지 않는다. 진행 중인 sync는 base를 붙들고 있고 해소 commit과
그것을 정리하는 hook 사이에서는 파일과 최신 trailer가 설계상 어긋나므로, 그
상태는 `sync` 검사가 자기 언어로 보고한다.)

- 기록된 base가 재유도 후보의 **후손**이면 아무 말도 하지 않는다. 게시 후
  전진 규칙·`pull`·`sync`가 base를 trailer가 가리키는 commit보다 앞으로
  옮기는 것은 정상이며, 방금 게시한 workspace는 모두 이 상태다.
- docs worktree가 `HEAD`와 다르면 재유도가 **의도적으로** 보류된 상태다.
  보고할 사실이지 문제가 아니므로 `[info]`다.
- 그 밖에는 재유도가 실행됐다면 다른 답을 냈을 상태, 즉 파일과 이력이 서로
  다른 말을 하는 상태다. `[warn]`으로 알리고 `--fix`가 재유도 값을 쓴다.

`--fix`의 base 재유도(`repairBase`)와 `sanho init`도 같은 규칙 아래 있다. note가
있는(또는 읽을 수 없는) 동안 `--fix`는 base를 쓰지 않고 `[info]`로 그 사실만
말하며, `sanho init`은 아예 거절한다 — `--force`가 docs 디렉터리를 canonical
내용으로 갈아치우고 base를 canonical head로 기록하는데, 그 뒤 abort가 docs만
되돌리면 base가 worktree보다 앞선 상태가 남기 때문이다.

base를 쓰는 경로는 여덟이다: `sync`(clean)·`--continue`·`--abort`의 복원·
`pull`·게시 후 전진·재유도·`doctor --fix`·`init`/`migrate`. 여덟 모두
§base 쓰기 가드 하나를 지나며, 그것이 불변식의 강제점이다.

## 게시 계약 (pre-push)

입력은 hook stdin의 ref update 목록, base 파일, private clone이다.

1. **필터.** `refs/heads/*`이고 local OID가 0이 아닌 update만 남긴다. tag
   push와 branch 삭제는 그대로 통과한다. local ref가 `HEAD`인 update는
   **먼저** 현재 branch 이름으로 치환한다(`git push origin HEAD` 대응);
   치환 전에는 branch update로 보이지 않기 때문이다. detached HEAD는 치환하지
   못하므로 필터에서 걸러진다. 남는 것이 없으면 exit 0.

   이 필터는 아래 어떤 단계보다도 먼저 돌고, 순서가 계약이다. tag만 담은
   push나 branch 삭제만 담은 push는 게시할 것이 없으므로, 자기와 무관한 sync
   창 때문에 거절돼서도 안 되고 clone을 만들고 fetch하는 `Ensure`를 기다려서도
   안 된다. 둘 다 실제로 일어났다 — 미완료 sync 중의 `git push --tags`가
   거절됐고, 오프라인에서 같은 push가 필요도 없는 network 호출로 실패했다.
2. **sync 진행 게이트.** `sync.json`이 있으면 거절한다. clone을 열기 전에,
   로컬 파일 하나만 보고 판정하므로 가장 값싼 거절이다.

   **게이트는 아무것도 쓰지 않는다.** note를 지우는 것은 `sanho sync
   --continue`와 `sanho sync --abort` 둘뿐이다(§감지 계약 1번). 예전에는
   "해소된 것처럼 보이면" 여기서 note를 정리했고, 그래서 stash로 마커를
   빠져나온 뒤 같은 파일을 계속 편집한 것이 완료로 읽혔다. 해소 commit을
   담은 push도 거절되며, 그것이 계약이다 — 완료는 명시적 행위이고, 완료되지
   않은 workspace의 base는 아직 그 docs를 설명하지 않는다.

   상태에 따라 네 가지로 갈린다. 어느 쪽이든 거절이고, 분류가 정하는 것은
   **어떤 문장을 출력할지**뿐이다.

   - 마커가 남아 있으면 진행 중인 sync다.
     ```text
     sanho: finish the sync first: resolve the conflicts, 'git add' and 'git commit', then 'sanho sync --continue' (or 'sanho sync --abort' to undo it)
     ```
   - 마커가 없고 docs가 clean인데 **충돌 경로를 바꾼 commit이 없으면**,
     해소가 commit되지 않았다는 뜻이다(stash·revert·`git checkout HEAD --
     docs`, 그리고 무관한 문서만 commit한 경우).
     ```text
     sanho: the sync from <a> to <b> is not completed; no commit has changed the files it conflicted on
     Run 'sanho sync --abort' to undo it — anything you stashed stays in your stash — then 'sanho sync' to lay the conflicts out again.
     If the docs already read the way you want them, run 'sanho sync --continue' instead to complete the sync as it stands.
     ```
     이 거절이 마지막 방어선은 아니다. 충돌 sync는 base를 옮기지 않으므로
     (아래 sync 계약 8번), 설령 이 게이트를 지나치더라도 push는 실제 이력에
     대해 case ③으로 평가된다. 게이트는 **무엇을 안내할지**를 정하고, base는
     **게이트가 틀렸을 때 무엇을 잃는지**를 정한다.
   - 해소가 commit됐지만 완료되지 않았거나, 아직 해소 중이면 그 사실을
     말한다.
     ```text
     sanho: the sync from <a> to <b> is not completed — the resolution is committed, and only 'sanho sync --continue' records it
     ```
   - note를 읽을 수 없으면 존재만으로 거절하고 abort를 안내한다.
     ```text
     sanho: the record of the sync in progress is unreadable (<원인>)
     Run 'sanho sync --abort' to restore the docs from HEAD, forget the docs base it cannot vouch for, and clear it.
     ```
3. **fetch.** `git -C <clone> fetch origin`. 실패하면 fail-closed로
   canonical-unreachable 메시지와 함께 거절한다.
4. **마커 게이트.** push되는 각 tip에 대해, **그 게시가 canonical에 새로
   들여오는** docs blob을 스캔한다. 범위는 canonical head의 docs tree와 tip의
   docs tree 사이의 diff다(양쪽 다 tree이므로, canonical commit이 docs 전용인
   덕분에 애플리케이션 저장소에서 그대로 비교된다). canonical이 비어 있거나
   그 tree object가 이 저장소에 없으면 tree 전체를 스캔한다 — 귀납을 신뢰할 수
   없을 때의 fail-closed 답이다. 하나라도 걸리면 파일 이름을 나열하고
   거절한다.

   기준점이 canonical head인 이유는 귀납이 거기서만 성립하기 때문이다.
   canonical head를 만든 모든 게시는 이 게이트를 먼저 통과했으므로, 그 tree에
   게이트를 우회한 내용이 들어갈 길이 없다. 이전 기준점이던 **애플리케이션
   remote의 직전 tip**에는 그런 성질이 없었다. `git push --no-verify` 한 번이면
   마커를 담은 commit이 게시 없이 code remote에 올라가고, 그 뒤로는 모든 push가
   "검증된 적 없는 tip"을 기준으로 diff를 뜬다. 마커는 영구히 스캔 범위 밖에
   남고 다음 평범한 push에서 조용히 게시된다. 비용 측면의 동기는 그대로다.
   이전 구현은 docs 파일 하나당 git 자식 프로세스 두 개를 썼고, 4,000개
   규모에서 commit 한 번이 39초였다.

   게이트가 fetch **뒤로** 옮겨진 것은 이 기준점 때문이다. 값싼 거절을 먼저
   한다는 원칙은 clone을 열기도 전에 판정할 수 있는 sync note 거절(2번)이
   가져간다.
5. **평가.** 이 단계는 **아무것도 게시하지 않는다.** canonical head를 한 번
   읽어 `(H, Ht)`로 고정하고, 그 하나의 스냅샷을 기준으로 모든 tip을 판정한
   뒤 각 tip이 게시하게 될 tree를 tree 수준 병합만으로 미리 계산한다. tip
   하나라도 충돌하거나, base가 없거나, canonical을 비우거나, rewrite된 이력
   위에 있으면 **canonical을 건드리기 전에** push 전체를 거절한다. 그래서
   거절 메시지의 "no remote ref was changed"가 구성상 참이다.

   tip별 docs tree `T`를 구하고 같은 `T`는 중복 제거한다. 같은 OID를 가리키는
   ref도 먼저 한 번만 남긴다. 기록된 base를 `B`라 하면:

   | case | 조건 | 동작 |
   |---|---|---|
   | ① `up_to_date` | `T == Ht` | 게시할 것이 없다. base 상태를 보지 않고 먼저 판정하므로, base가 없거나 고아여도 docs가 같은 push는 통과한다. |
   | ② `fast_forward` | `B == H`이고 `T != Ht` | clone에 tip을 import하고 `T`를 tree로 하는 commit을 `H` 위에 만들어 push한다. |
   | ③ `auto_merge` | `B`가 `H`의 진짜 조상 | clone에서 3-way 병합(base=`B`의 docs tree, ours=`T`, theirs=`Ht`)을 계산한다. clean이면 병합 tree를 게시하고 push를 그대로 진행한다. 충돌이면 파일을 나열하고 `sanho sync`를 안내하며 거절한다. |
   | ④ `unknown_base` | `B`가 canonical에서 해석되지 않거나 게시 branch 위에 없다 | `docs-base-tree`와 같은 docs tree를 가진 canonical commit을 찾아 재유도(re-anchor)한다. 찾으면 그것을 `B`로 삼아 다시 판정한다. 못 찾으면 rewrite 복구 메시지로 거절한다. |

   base가 아예 없고 canonical이 비어 있지도 않으면 `sync_required`(사유
   `no_base`)로 거절한다. 병합 base가 없으면 안전하게 합칠 방법이 없고,
   `sanho sync`가 그 상태에서 base를 만들어 주기 때문이다.

   **case ②는 tip의 이력이 base를 뒷받침해야 성립한다.** fast-forward는 병합
   없이 tip의 docs tree를 canonical head **위에 그대로** 게시하는 유일한
   case이므로, canonical이 들고 있고 tip이 들고 있지 않은 것은 전부 삭제된다.
   그 근거는 오직 기록된 base다 — `B == H`는 "이 docs가 파생된 이후 상류에
   들어온 것이 없다"는 뜻이니까.

   base 파일 혼자서는 그 근거를 감당하지 못한다. base 파일은 checkout 루트의
   **workspace 상태**이고 게시되는 것은 **branch**다. sanho 도입 이전 docs를
   가진 branch나 provenance가 한 번도 새겨지지 않은 branch를 체크아웃하면,
   파일은 여전히 canonical head를 이름짓는데 그 밑의 문서는 거기서 파생된 적이
   없다. 그것이 4차 리뷰 C2의 재현이다: 낡은 문서 하나짜리 branch가 canonical의
   여섯 문서를 전부 대체했고, `(fast_forward)`로 보고됐다.

   그래서 tip이 **자기 이력으로** base를 보증해야 한다. tip에서 도달 가능한
   최신 `docs-base` trailer가 기록된 base를 이름짓거나, 그 canonical **조상**을
   이름지어야 한다. 조상 쪽은 빠져나갈 구멍이 아니라 정상 상태다 — 게시 후 전진
   규칙이 base 파일을 trailer가 가리키는 commit보다 앞으로 옮기므로, 방금
   push한 workspace는 모두 그 상태다. 보증되지 않으면 `sync_required`(사유
   `uncorroborated_base`)로 거절하고 `sanho sync`를 안내한다. case ③은 양쪽을
   합치므로 이 요구가 필요 없고, case ①은 base를 보기도 전에 통과한다.

   **다중 ref push는 사슬로 이어진다.** 두 번째 이후의 tip은 canonical의
   고정된 head tree가 아니라 **앞선 tip이 게시하게 될 tree** 위로 병합한다
   (병합 base는 고정된 `B`의 docs tree, 없으면 empty tree). 판정이 ②라도
   그렇다. 두 번째 tree를 그대로 fast-forward하면 첫 번째 branch가 더한 문서가
   통째로 삭제되고, 그것도 exit 0으로 삭제된다. `git push origin main topic`
   한 번이나 `git push --all` 한 번이 그 상황이다. stdin 순서는 사슬의 순서만
   정하고 결과 내용은 정하지 않는다 — 병합은 합집합이므로 어느 순서로도 같은
   tree에 도달한다.

   **docs 없는 tip은 거절한다.** tip의 docs tree가 empty tree인데 사슬이 도달한
   tree는 비어 있지 않다면, 게시는 canonical의 모든 문서를 삭제하는 일이 된다.
   docs 디렉터리가 생기기 전에 만들어진 branch와 `git rm -r docs`가 요점이었던
   branch는 이 지점에서 구별되지 않으므로 fail-closed로 거절하고 branch 이름과
   삭제될 문서 수를 말한다. 의도한 삭제라면 그 push 한 번에 한해 환경 변수
   `SANHO_ALLOW_DOCS_DELETION=1 git push` 접두 형태로 명시한다(값은 프로세스
   환경에서 읽으므로 `export`는 그 셸의 이후 push 전부에 적용된다 — 접두
   형태만이 실제로 한 번짜리다). 삭제 자체는 정당한 작업이고, 다만
   추론할 일이 아니다.

6. **부트스트랩.** canonical 게시 branch에 commit이 하나도 없으면
   `ErrEmptyBranch`로 감지하고, head tree 자리에 empty tree를 넣어 판정한다.
   기록된 base는 이때 의도적으로 무시한다. canonical이 애초에 비어 있는데
   "이력이 rewrite됐다"고 진단하는 것은 거짓이고, 합칠 상류 내용도 없기
   때문이다. tip에 docs가 없으면 ①, 있으면 ②로 parent 없는 root commit을
   만들어 lease 없이 push한다.
7. **게시.** 평가가 끝난 tree들을 순서대로 commit하고 push한다. 각 게시의
   parent는 직전 게시의 commit이다. 이 단계에는 결정할 것이 남아 있지 않다.
   게시한 OID는 하나도 빠짐없이 보고한다 — 마지막 것만 보고하던 동안 다중 ref
   push의 내용 손실이 보이지 않았다.
8. **CAS와 재시도.** push는
   `--force-with-lease=refs/heads/<branch>:<expectedOld>`로 수행한다.
   expectedOld를 원격에 전송하므로 조건 검사를 **서버가** 수행한다. 거절
   신호(`stale info`, `[rejected]`, `non-fast-forward`, `fetch first`,
   `cannot lock ref`)를 만나면 refetch 후 case 분석을 처음부터 다시 한다.
   병합 결과를 재사용하지 않고 반드시 새 head 기준으로 다시 계산한다.
   최대 3회(`MaxCASRetries`) 시도하고 소진되면 `cas_retry_exhausted` 사유로
   `sanho sync`를 안내하며 거절한다. `--force`는 어떤 경로에서도 쓰지 않는다.
   재유도(④)는 CAS 시도를 소비하지 않는다. 로컬 상태 정정이지 경합 패배가
   아니기 때문이다. 경합에 지면 평가부터 다시 한다.
9. **base 전진.** 게시에 성공하면 게시된 OID를 출력한다.
   ```text
   sanho: published docs <oid12> (<case>)
   ```
   그 뒤 worktree docs tree가 방금 게시한 tree와 **완전히 같을 때만** base
   파일을 새 canonical commit으로 옮긴다. case ③의 병합 결과처럼 worktree가
   아직 보지 못한 내용이거나 미commit 편집이 있으면 base는 그대로 둔다.
   base 전진은 push 한 번에 한 번, 마지막 게시를 기준으로만 일어난다.

### worktree 불가침

pre-push는 working tree, index, 애플리케이션 ref를 절대 바꾸지 않는다.
git 자신의 push 의미론과 같다. case ③에서 clean 병합으로 게시한 뒤에도
worktree는 그대로 두므로 `status`는 "behind(내 병합 결과)"로 보인다. 이때
따라잡는 명령은 `sanho sync`다. `sanho pull`은 worktree docs tree가 base
tree와 정확히 같을 것을 요구하는데, 방금 게시한 병합 결과는 정의상 worktree가
아직 보지 못한 tree여서 pull은 거절한다. 병합 결과를 흡수하는 일은 소비가
아니라 재조정이므로 sync가 맞는 동사다.

### canonical commit 규약

게시 1회당 commit 1개다.

```text
subject: docs: <repo-name>/<branch> (<N> app commits)

source: <workspace-id> @ <app tip OID>
commits:
  - <base 이후 docs를 건드린 각 app commit의 subject>
```

- `<repo-name>`은 origin URL의 마지막 segment에서 `.git`을 뗀 이름이고,
  origin이 없으면 worktree 디렉터리 이름이다.
- `<branch>`는 push된 branch이며, detached HEAD면 `HEAD`다.
- `<N>`은 나열된 subject 개수이고 복수형 처리를 하지 않는다. 기계가 맞추는
  형식이므로 `(1 app commits)`도 정상 출력이다.
- subject 목록의 범위는 **push되는 ref의 직전 remote tip 이후**다. hook이
  받는 remote OID를 그대로 쓰며, 그 ref를 remote가 처음 보는 경우(새 branch)
  나 rewrite로 그 commit이 사라진 경우에는 tip에서 도달 가능한 docs commit
  전체가 대상이 된다. base 이후가 아니다. 어느 쪽이든 최대 100개
  (`MaxSubjects`)까지 최신 것부터 잘라내고, 나열은 오래된 것부터 한다. docs를
  건드린 commit이 없으면 `commits:` 절 자체를 생략한다.
- author와 committer는 모두 `.sanho.json`의 `actor_email`이다. 이름이 없으면
  주소의 local part, 그것도 없으면 `sanho`를 쓴다.
- canonical 이력은 선형으로 유지한다. merge commit을 만들지 않는다.

## 병합 원시 연산

tree 수준 3-way 병합은 `git merge-tree -z --write-tree`로 수행한다.

```bash
git merge-tree -z --write-tree --merge-base=<baseCommit> sanho-ours sanho-upstream
```

- 세 tree는 각각 parent 없는 synthetic commit으로 감싼다. author/committer
  이름·메일·시각을 고정값으로 못박으므로 같은 tree 세 개는 언제 어디서나 같은
  결과 tree를 만든다.
- ours/theirs 쪽은 `refs/sanho-ours`, `refs/sanho-upstream`에 임시로 매단다.
  마커 라벨이 임시 경로나 raw OID가 아니라
  `<<<<<<< sanho-ours` / `>>>>>>> sanho-upstream`으로 읽히게 하기 위해서다.
  임시 ref는 context가 취소돼도 반드시 정리한다.
- exit 0은 clean, exit 1은 "충돌과 함께 병합됨"이다. 그 밖의 값만 실제
  실패다. 이것은 `git merge-file`의 계약(1–127이 충돌 *개수*)과 다르며,
  두 계약을 섞어 읽는 것이 v0.1의 최악 결함이었다.
- `-z` 출력을 파싱해 결과 tree와 충돌 경로를 얻는다. 충돌 경로는 stage
  정보 절에서 먼저 읽고, 비어 있을 때만 정보 메시지 절에서 `CONFLICT` 항목의
  경로를 읽는다.
- 실행 위치는 세 tree가 모두 있는 저장소다. 게시는 private clone에서,
  sync는 애플리케이션 저장소에서 실행한다.

### git 버전 정책

버전을 강제하지 않는다. `sanho init`은 버전을 검사하지 않고, `sanho doctor`는
감지한 버전을 정보로만 보고한다.

```text
[ok  ] git              git version 2.x.y (no minimum is enforced; merges need git 2.38 or newer)
```

`merge-tree --write-tree`는 실무상 git 2.38 이상이 필요하다. 더 낮은 git에서는
git 자신의 명확한 오류가 그대로 표면에 드러나며, Sanho는 대체 구현을 미리
만들어 두지 않는다.

### 마커 탐지

판정 순서는 **분류가 먼저, 크기가 나중**이다. 언제나 앞 8 KiB를 읽어 NUL
바이트로 binary 여부를 정하고, binary면 크기와 무관하게 건너뛴다. docs 아래의
그림·PDF·녹화는 충돌 마커를 담을 수 없고, 크다는 이유만으로 commit 경로를 막을
이유가 없다. `MaxScanSize`를 넘는 **텍스트**만 파일 이름을 말하는 오류다(감사
H2가 요구한 fail-closed). 크기를 넘은 object는 앞부분만 읽어 분류하므로,
기가바이트짜리 파일을 메모리에 올리는 일은 일어나지 않는다.

게시 게이트와 commit/sync 게이트가 같은 탐지기를 쓴다.

- 대상은 git blob(tip 스캔)과 worktree 파일(sync 진행 중 스캔)이다.
- 앞 8 KiB 안에 NUL 바이트가 있으면 binary로 분류하고 건너뛴다.
- 줄 길이 제한이 없다. 아주 긴 줄이 뒤따르는 마커를 가리지 못한다.
- 세 마커가 **줄 시작에서, 순서대로** 나타나야 충돌로 판정한다.
  `<<<<<<< `, 정확히 `=======` 한 줄, `>>>>>>> `. CRLF의 `\r`만 허용한다.
- 10 MiB(`MaxScanSize`)를 넘는 파일은 조용히 통과시키지 않고 오류로 보고한다.
- 읽기 실패는 그대로 전파해 게이트를 fail-closed로 만든다.

## sync / pull 계약

동사가 둘인 이유는 의도가 둘이기 때문이다. `pull`은 소비 전용 빠른 경로이고,
`sync`는 화해(reconcile)다.

### `sanho sync`

1. sync note가 이미 있으면 거절한다. docs 경로가 worktree와 index 양쪽에서
   `HEAD` 기준 clean해야 한다. docs 밖은 보지 않으므로 사용자의 비docs 작업은
   그대로 유지된다. docs가 dirty하면 거절한다.
   ```text
   sanho: docs have uncommitted changes: commit or stash your docs changes first
   ```
   note를 먼저 검사하는 순서가 중요하다. 충돌 sync는 구조상 docs를 dirty하게
   만들기 때문에, 순서를 바꾸면 "docs 변경을 commit하라"는 쓸모없는 안내가
   나온다.
2. canonical을 fetch한다. 실패하면 fail-closed다(쓰기 경로).
3. canonical에 commit이 하나도 없으면 소비할 상류가 없다는 뜻이므로 그대로
   up-to-date로 보고한다. `--rebase-onto`를 함께 준 경우에만 거절한다.
   ```text
   sanho: canonical repository has no commits yet; nothing to sync
   ```
4. canonical head를 애플리케이션 저장소의 object database로 import한다.
   head에서 도달 가능한 모든 것이 함께 오므로, 기록된 base나 `--rebase-onto`
   대상도 별도 import 없이 해석된다.
5. base tree를 정한다.
   - 기록된 base가 **없으면 empty tree**를 쓴다. empty base 위의 3-way 병합은
     양쪽 추가의 합집합이 되고, 같은 경로를 서로 다르게 추가한 곳에서만
     충돌한다. 게시가 `no_base`로 사용자를 여기로 보내므로, 이 상태에서
     `sanho sync`가 실제로 성공해야 안내가 닫힌다.
   - base commit이 canonical head에서 **도달 가능하면** 그 commit의 tree를
     애플리케이션 저장소에서 해석해 쓴다. 기록된 tree 필드보다 이쪽을 우선한다.
     병합이 실행되는 저장소에 object가 실제로 있음을 증명하고, 낡은 tree 필드가
     병합 base를 오염시키지 못하게 하기 때문이다. legacy `{commit, tree:""}`
     base도 특별 취급 없이 이 경로로 처리된다.
   - 도달 불가능하면 기록된 `docs-base-tree`로 canonical 이력을 검색해
     재유도한다. 기록된 tree가 없거나 어떤 commit도 그 tree를 갖지 않으면
     — **`--rebase-onto`로 대상을 명시했을 때는 empty tree로 폴백하고**,
     그러지 않았을 때만 `--rebase-onto`를 안내하며 거절한다. 폴백이 있는
     이유는 안내의 닫힘이다. 거절 메시지가 지목하는 그 명령이 바로 그
     상태에서 실패하면 안 되고, 기록된 이력과 canonical 이력이 정말로 아무것도
     공유하지 않을 때 정직한 공통 조상은 empty tree다.

     empty base 위의 병합은 합집합이므로 대가가 하나 있다. **canonical이
     삭제한 문서가 로컬에 남아 있으면 되살아난다.** 공통 조상이 없으면 "저쪽이
     지웠다"와 "이쪽이 추가했다"를 구분할 방법이 없기 때문이다. 되살리고 싶지
     않은 문서는 병합 뒤 지우고 commit하면 된다.
6. 병합(base tree, ours=`HEAD`의 docs tree, theirs=대상 tree)을 애플리케이션
   저장소에서 실행한다.
7. **clean**: 결과 tree를 `docs/`와 index에 반영하고(docs 경로만), base 파일을
   대상으로 갱신하고, `docs: sync to <oid12>` commit을 docs pathspec으로만
   만든다. 결과 tree가 `HEAD`의 docs tree와 같으면 commit할 것이 없으므로
   base 포인터만 옮긴다. base까지 이미 대상과 같으면 up-to-date로 보고한다.
8. **충돌**: 마커가 들어간 결과 tree를 `docs/`에 반영하고, `sync.json`에
   `{prev_base, target, started_at, entry_head, entry_docs_tree, conflicts}`를
   기록한다. **base 파일은 건드리지 않는다.**

   여기서 base를 대상으로 선-전진시키는 것이 v0.2 첫 구현의 구조적 결함이었다.
   해소가 밀려 있는 동안 workspace는 `base == canonical head`인데 docs
   worktree는 병합 이전 내용을 들고 있는 상태가 된다. 그러면 note가 힘을 잃는
   경로 — 해소로 오판된 stash, 읽을 수 없어 지워지기만 한 note — 어느 쪽이든
   다음 push가 fast-forward로 판정돼 병합 이전 tree를 상류에 되돌려 쓴다.
   exit 0, 메시지 없음.

   기다린다고 잃는 것은 없다. 대상은 note의 `target`에 그대로 있고, base는
   자기 정의(§상태 파일의 불변식 — "worktree docs가 어느 canonical 상태에서
   파생됐는가")에 계속 정직하게 답한다. 창 동안 그 답은 여전히 직전 base다.
   대상을 base 파일에 쓰는 것은 `sanho sync --continue` 하나뿐이다(아래).
9. 충돌 sync는 **오류가 아니다.** 요청받은 일을 했고 마커는 worktree에 있다.
   exit code는 0이고 `--json`의 `status`가 `conflicts`다.

### 해소와 완료 — `sanho sync --continue`

해소는 표준 git 관용구다. 편집 → `git add` → `git commit`. **완료는 그것과
별개의 명시적 행위다.** `sanho sync --continue`가 sync를 끝내며, 그 밖의
어떤 경로도 sync를 끝내지 않는다.

#### 왜 추론을 버렸는가 (D3 이탈)

세 차례의 검토가 "해소되었는가"를 사후 트리 증거로 추론했고, 세 번 모두 같은
방으로 통하는 문이 조금씩 작아졌을 뿐이다. 마지막 재현이 논거다. 마커를
`git stash push -- docs`로 치운 다음 **같은 문서를 계속 편집해서 commit하면**
— stash 이탈 후 가장 자연스러운 다음 행동이다 — HEAD가 움직이고, docs tree가
움직이고, 병합이 해결하지 못한 경로가 바뀐다. 사후 증거가 물을 수 있는 모든
질문이 "해소됨"이라고 답하는데 병합은 시작한 자리에 그대로 있다. 이것을
거절할 만큼 좁힌 술어는 정당한 해소도 거절하기 시작한다.

그래서 완료를 **사용자의 선언**으로 옮겼다. `git rebase --continue`와 동형이라
학습 비용이 사실상 없고, 대안은 데이터 손실이다. 이는 D3의 "새 어휘 없음
(resolve → add → commit)"에서 한 단계 벗어난 것이며, 의도적 이탈로 여기에
기록한다. 충돌 메시지는 세 줄이 되었다.

```text
sanho: merged docs with upstream — 1 files have conflicts:
  docs/api.md
Resolve the markers, then:  git add docs/ && git commit
Then complete the sync:     sanho sync --continue
To undo this sync:          sanho sync --abort
```

#### `sanho sync --continue`

전제는 넷이며, 각각이 남은 것을 이름짓는다.

1. sync note가 있다. 없으면 끝낼 것이 없다.
2. worktree docs 어느 파일에도 마커가 남아 있지 않다. 마커가 남은 tree에 대해
   base를 기록하는 것은 아무것도 해소하지 않은 상태를 기록하는 일이다.
3. docs가 `HEAD` 기준 clean하다. base가 서술할 대상이 편집 중인 내용이 아니라
   commit된 내용이어야 하기 때문이다.
4. **`HEAD`가 sync를 시작한 이력 위에 있다** — note의 `entry_head` 그 자체이거나
   그 자손이다. 로컬 `git merge-base --is-ancestor` 한 번으로 판정하며 network를
   열지 않는다.

네 번째가 4차 리뷰의 C1이고, 앞의 셋이 그 이유다. 셋 모두 **worktree**에 대한
질문인데, branch 전환은 셋을 전부 만족시키면서 문서를 통째로 바꾼다.
`git stash push -- docs`가 마커와 dirty를 없애고, `git checkout other`가 병합에
참여한 적 없는 이력으로 옮긴다. 그러면 완료는 그 문서들이 한 번도 화해한 적
없는 canonical head를 base로 기록했고, 다음 push는 fast-forward로 평가돼 상류를
덮어썼다 — exit 0으로.

동일성이 아니라 **조상 관계**가 옳은 판정이다. 해소는 보통 여러 commit이고,
때로는 해소에서 낸 branch 위에서 끝나며, 그 전부가 시작 지점의 자손이다. 자손일
수 없는 것은 애초에 참여하지 않은 branch다. `entry_head`가 비어 있는 note
(unborn HEAD, 또는 필드가 생기기 전 빌드가 남긴 note)는 어떤 이력도 주장하지
않으므로 위반될 수도 없어 통과시킨다.

여전히 **"충돌 경로를 바꾼 commit이 있는가"는 묻지 않는다.** 모든 충돌 경로를
바이트 그대로 "ours"로 두는 해소는 흔적을 남기지 않으며, 이전 설계에서 그것은
빠져 나갈 수 없는 막다른 길이었다(abort → 재sync → 같은 충돌). 빠져 있던 증거가
바로 **sync 자신의 이력 위에 선 사용자의 선언**이다.

성립하면 **note를 지우고, 그 다음 base 파일에 `target`을 쓴다.** commit을
만들지 않고(P3), ref를 움직이지 않고, network를 열지 않는다. base 쓰기는
§base 쓰기 가드를 통과하며, 가드는 같은 조상 관계를 어댑터 쪽에서 한 번 더
증명한다 — 불변식이 이 함수 하나에 기대지 않도록.

```text
sanho: sync completed; docs base is now <oid12>
```

완료되는 tree가 병합 결과와 다르면 **몇 개가 다른지 한 줄로 보고한다**(차단이
아니다). 충돌과 함께 clean했던 부분까지 되돌린 뒤 완료하는 것은 정당한
"내 줄을 유지한다" 선언이면서, 동시에 사용자가 충돌을 본 적조차 없는 상류
내용을 조용히 버리는 일이기도 하다. 숫자를 말하는 것이 결정과 사고의 차이다.

```text
sanho: sync completed; docs base is now <oid12>
2 files differ from the merge result and were completed as they stand.
```

전제가 성립하지 않으면 남은 것을 이름지어 거절한다.

```text
sanho: the sync is not ready to be completed (the docs worktree still contains conflict markers: docs/api.md)
Finish the resolution with 'git add docs/ && git commit', then run 'sanho sync --continue' again.
Or run 'sanho sync --abort' to undo the sync.
```

읽을 수 없는 note, 그리고 `target`이 없는 note는 무엇을 채택해야 할지 말할 수
없으므로 같은 방식으로 거절하고 `--abort`를 안내한다. 추측하지 않는 것이
불변식의 요구다.

#### base "앞서지 않음" 불변식

이 파일 전체를 지배하는 규칙 하나가 있다.

> **기록된 base는 worktree docs보다 앞설 수 없다. 둘 다 확정할 수 없으면 더
> 오래된 값을 택한다.**

base가 너무 오래되면 대가는 병합 한 번이다. 게시가 실제 이력에 대해 판정하고,
최악의 경우 충돌을 보고한다. base가 너무 새로우면 대가는 상류의 작업이다.
다음 push가 fast-forward로 판정되어 worktree가 들고 있는 것을 그대로 게시한다.
두 실패는 비교 대상이 아니므로 모든 판단을 오래된 쪽으로 해소한다.

불변식이 구현에 남긴 자리는 다섯이다.

- **쓰기 순서**: `--continue`는 note 삭제 → base 기록 순이다. 중간에 죽으면
  note는 사라지고 base는 **직전 값**에 남는다(게시가 case-③ 병합으로 화해할
  수 있는 상태). 반대 순서는 base가 앞선 채로 죽는다.
- **창 안 stamping**: 창 안의 모든 commit은 **base 파일 값**을 새긴다. 아래
  참조.
- **읽을 수 없는 note의 abort**: `prev_base`를 알 수 없으므로 base 파일을
  **지운다**. 아무 base도 없는 것이 가장 오래된 값이다.
- **다른 base 기록자**: 재유도(§base 재유도), `sanho doctor --fix`,
  `sanho init`은 note가 있는 동안 base를 쓰지 않는다.
- **base 쓰기 가드**: base를 기록하는 **모든** 경로가 하나의 함수를 지난다.
  아래.

#### base 쓰기 가드 (강제점)

같은 실패 계열이 네 번 재발했고, 매번 다른 경로였다. 매번 그 자리에서는
올바르게 보이는 쓰기였다 — 값의 출처가 실재했고, 호출자에게 이유가 있었고,
결과는 옆에 놓인 문서가 한 번도 파생된 적 없는 canonical 상태를 가리키는
base 파일이었다.

세 웨이브가 세 호출자를 고쳤다. 바뀌지 않은 것은 `wsstate.SaveBase`가 **무조건
쓰기**라는 사실이다. 위 불변식은 런타임 강제가 어디에도 없었고, 호출자 전원이
각자 옳게 하는 것으로만 유지됐다. 내년에 쓰일 열 번째 호출자는 앞선 세 수정을
하나도 물려받지 않는다.

그래서 불변식에 **강제점**을 두었다. `internal/interface/cli`의 `writeBase`가
`wsstate.SaveBase`를 호출할 수 있는 유일한 곳이고, `internal/architecture`의
테스트가 다른 호출자가 생기는 순간 빌드를 깬다.

가드는 "worktree가 이 내용을 담고 있는가"를 묻지 않는다. 그것은 틀린 질문이다 —
문서 하나를 지우고 게시하는 것은 정당한 작업이고, 그때 base는 worktree에 없는
내용을 이름짓는다. 가드가 묻는 것은 **"이 workspace가 자기 문서의 출처를
로컬로 보일 수 있는가"**이며, 스스로 확인할 수 있는 증명에만 통과를 준다.
증명은 다음뿐이고, "호출자가 그렇다고 한다"는 없다.

| 증명 | 내용 | 통과하는 경로 |
| --- | --- | --- |
| worktree tree | 후보의 docs tree가 worktree가 해시하는 tree 그 자체다. | 게시 advance, `pull`, `init`(fresh), 병합 결과가 곧 대상인 sync |
| 같은 commit | 기록된 base가 이미 그 commit을 이름짓는다(쓰기가 아무것도 옮기지 않는다). | `migrate`의 tree 보강, 값을 재확인하는 재유도 |
| 이력 stamp | `HEAD`에서 도달 가능한 최신 `docs-base` trailer가 이 commit을 이름짓는다. | 재유도, `doctor --fix`, `init`(reuse) |
| 더 오래된 base | 후보가 기록된 base의 canonical 조상이다(뒤로 가는 것은 언제나 안전하다). | `--abort`의 복원, `--rebase-onto` |
| 흡수됨 | 후보의 docs를 worktree docs에 3-way 병합해도 clean하고 아무것도 바뀌지 않는다. 병합 base는 기록된 base의 tree와 **빈 tree** 둘 다 시도한다 — 후자는 흐름 자신이 쓴 조상이며(rewrite 복구), 더 강한 주장이다. | `sanho sync`의 clean 경로 |
| tree 미해결 | 후보의 docs tree가 이 저장소에 아예 없어 어느 방향으로도 비교가 불가능하다. 잃을 것을 보일 수 없으므로 통과시키고, 게시 쪽 확증이 뒤를 받친다. | legacy 채택 |
| sync 완료 | 완료하려는 sync가 지금 `HEAD`가 선 이력에서 시작됐고 docs가 commit돼 있다. | `sanho sync --continue` |

마지막 하나만 호출자가 증거를 건넨다. 이유는 tree로는 결코 알 수 없는 사실이기
때문이다 — "내 줄을 전부 유지한다"는 해소는 worktree를 병합 이전과 바이트
동일하게 남기므로, 어떤 tree 비교도 그것을 "참여한 적 없는 branch에서의 완료"와
구별하지 못한다. 그래도 **조상 관계 자체는 여기서 실제 git으로 검증**한다;
넘어오는 것은 note에서 방금 읽은 시작 commit뿐이다.

증명이 하나도 서지 않으면 **쓰지 않고 거절한다.** 흐름은 그 거절을 그대로
보고하고, 재유도는 대신 **지운다**(아래).

가드가 마지막 방어선은 아니다. base 파일은 손으로 고칠 수도, 백업에서 되돌릴
수도, 이 코드를 한 번도 지나지 않은 도구가 들여놓을 수도 있으므로, 게시가
사용 시점에 같은 요구를 한 번 더 한다(§게시 계약의 fast-forward 확증).

#### 창 안 stamping은 언제나 base 파일 값이다

`commit-msg`가 새기는 `docs-base` trailer는 창 안에서도 base 파일 값이다.
해소 commit도 예외가 아니다.

이전 구현은 해소 commit에만 병합 **대상**을 새겼다. 그 commit의 docs는 대상에서
병합된 내용을 담고 있으니 참으로 보였지만, **trailer는 그것을 만든 sync보다
오래 산다.** `sanho sync --abort`(도구가 직접 안내하는 명령) 뒤에도 그 commit은
이력에 남고, 브랜치 전환 한 번이면 재유도가 그 trailer를 채택해 base를 대상에
올려놓는다. 그 밑에는 병합 이전 docs가 있다. 재유도 stand-down 가드는 note가
있는 동안에만 유효하고, abort는 방금 그 note를 지웠다.

base 파일 값을 새기면 거짓도 위험도 아니다. 그 값은 커밋된 docs의 참인 조상이며
(창 시작 시 worktree가 파생된 상태), 나중에 재유도가 그것을 채택해도 base는
기껏해야 **너무 오래된** 쪽으로 틀리므로 게시가 평범한 분기로 화해한다. 대상은
`--continue`를 통해서만 base 파일에 들어온다.

이 때문에 재유도 stand-down 가드는 **안전의 최후 방어선이 아니게 되었다.**
가드는 유지하지만(창 안에서 base를 흔드는 잡음을 막는다) 그것이 뚫려도 손실은
없다.

#### 진행 중인 sync의 상태 보고

완료가 명시적 행위가 된 뒤에도, 끝나지 않은 sync가 어떤 모양인지는 사용자에게
말해야 한다. 보고 전용 분류는 다섯이다.

| 분류 | 상태 | 어디서 나오는가 |
|---|---|---|
| `no_sync` | note 없음 | 아무 말도 하지 않는다 |
| `pending` | 마커가 남았거나 docs가 dirty | 마커가 있으면 commit을 막는다 |
| `not_committed` | 마커 없음·clean인데 충돌 경로를 바꾼 commit이 없음 | stash·revert·`checkout HEAD -- docs` |
| `resolved` | 충돌 경로를 바꾼 commit이 있음 | 해소했고 아직 완료하지 않음 |
| `unknown` | note가 답할 수 없음(구식 note, `conflicts` 없음) | 업그레이드를 가로지른 workspace |

**이 분류는 아무것도 완료시키지 않는다.** 잘못 읽어도 대가는 잘못된 문장
하나이고, base 쓰기는 어느 쪽으로도 일어나지 않는다. 이전 설계에서 같은 판정이
note를 지우고 base를 옮겼으며, 그 판정을 hook이 수행했다 — 읽기 경로가 창의
정의 그 자체인 파일을 변경하고 있었다.

`unknown`이 `not_committed`와 갈라지는 이유는 문장 하나다. `conflicts`를 담지
않은 note에 대해 "no commit has changed the files it conflicted on"이라고 말하는
것은 아무도 알지 못하는 사유를 진술하는 일이다. 그래서 구식 note는 그 사유
없이 보고하고, `--continue`로 정상 탈출한다(예전에는 abort만 가능했다).

같은 상태를 `sanho status`와 `sanho doctor`도 자기 자리에서 말한다. 창 안에서
`status`는 behind 줄 대신 sync 줄을 낸다. behind 수치는 창 동안에도 참이지만,
`N behind — 'sanho sync' will merge cleanly`는 note가 있는 동안 거절되는 명령을
이름짓기 때문이다(D3 위반). 두 명령 모두 창을 끝내는 두 명령만 이름짓는다.

```text
sync      : IN PROGRESS — complete it with 'sanho sync --continue', or undo it with 'sanho sync --abort'
```

### `sanho sync --abort`

note가 있으면 언제나 유효하다. **읽을 수 없는 note도 포함이다.** abort는 망가진
sync 상태에서 빠져나오는 수단이므로 손상된 note야말로 견뎌야 하는 경우다.
거절하면 docs에 마커가 남고, 아무도 파싱하지 못하는 파일이 남고, 둘 중 어느
쪽도 치울 명령이 없는 workspace가 된다.

1. `docs/`를 `HEAD` 기준으로 복원한다.
2. base 파일을 정리한다. note를 읽을 수 있으면 `prev_base`로 되돌리며, 충돌
   sync가 base를 옮기지 않으므로 이 단계는 보통 **디스크에 이미 있는 값을 다시
   쓰는 멱등 연산**이다. 실제로
   의미가 있는 경우는 둘뿐이다.
   - base를 선-전진시키던 이전 빌드가 남긴 옛 형식 note(`entry_head`가 없는
     note). 그 workspace는 base가 정말로 대상에 가 있으므로 `prev_base`를 다시
     써야 한다. 옛 note와 새 note의 **유일한 비대칭**이며, 데이터를 잃지 않는
     방향으로만 작동한다.
   - base 없이 sync에 들어간 경우. `prev_base`가 비어 있고 빈 base는 파일
     스키마로 표현할 수 없으므로 base 파일을 삭제한다.
3. note를 지운다.

note를 읽지 못한 경우 2단계는 **base 파일 삭제**가 된다. `prev_base`가 note
**안에** 있었으므로 추측할 수 없고, 추측하지 않는다는 것이 불변식의 요구다.

건너뛰기(base를 그대로 두기)는 "충돌 sync는 base를 옮기지 않는다"는 전제 위에
있었고, 그 전제가 깨지는 상태가 둘 있다. `SaveBase`와 `ClearSyncNote` 사이의
크래시, 그리고 충돌 시점에 base를 선-전진시키던 빌드가 남긴 note다. 두 경우
모두 abort는 병합 대상에 올라간 base와 그 밑의 병합 이전 docs를 남기고 떠났고,
다음 push가 exit 0으로 상류를 덮었다.

base가 없는 workspace는 막힌 상태가 아니다. 게시는 `no_base`로 거절하며
`sanho sync`를 안내하고, base 없는 sync는 empty tree를 병합 base로 삼아 실제로
성공한다. 무음 fast-forward는 없다.

ref를 움직이지 않고 commit을 만들지 않으며 docs worktree/index와 상태 파일
둘만 건드린다. 그래서 **실패할 수 있는 상태가 존재하지 않는다.** 순서도 재실행
가능하도록 정했다. note가 마지막에 지워지고 그 앞 단계는 모두 멱등이므로,
중단 후 다시 실행하면 그대로 이어진다.

```text
sanho: sync aborted; docs restored to HEAD
```

### `sanho sync --rebase-onto <commit>`

rewrite 복구용이다. 대상은 canonical에 존재하는 commit이어야 한다. canonical이
게시한 적 없는 것을 향해 병합하면 다음 push가 쓸 수 없는 base를 기록하게
되기 때문이다. 나머지 흐름은 동일하다. `--abort`와 함께 쓸 수 없다.

### `sanho pull`

소비 전용이다.

1. sync가 진행 중이면 거절한다.
2. fetch하고, canonical이 비어 있으면 up-to-date로 보고한다.
3. base가 없으면 `sanho sync`를 먼저 실행하라고 거절한다.
4. worktree docs tree가 base tree와 **정확히 같아야** 한다. 다르면 거절한다.
   ```text
   sanho: local docs have changes that 'sanho pull' cannot fast-forward: local docs differ from base <oid12>; run 'sanho sync' to reconcile them
   ```
5. 대상 tree를 `docs/`와 index에 반영하고 base 파일을 갱신한다.
6. `--commit`은 대상 tree가 `HEAD`의 docs tree와 다를 때만 `docs: sync to
   <oid12>` commit을 만든다. 같으면 기록할 변경이 없다.

## 감지 계약 (pre-commit)

commit 경로는 network를 열지 않고 canonical 가용성에 의존하지 않는다.

1. sync note를 먼저 읽는다. **읽기만 한다.** hook은 note를 쓰지도 지우지도
   않는다. 지우는 것은 `sanho sync --continue`와 `sanho sync --abort` 둘뿐이다.
   예전에는 바로 여기서 "해소된 것처럼 보이면" note를 지웠고, 그래서 stash
   이탈 후 같은 파일을 계속 편집한 것이 완료로 읽혔다.
2. note가 남아 있고 worktree docs에 마커가 있으면 commit을 막는다.
   ```text
   sanho: a sync is in progress — N files still have conflicts:
     docs/api.md
   Resolve the markers, then:  git add docs/ && git commit
   Then complete the sync:     sanho sync --continue
   To undo this sync:          sanho sync --abort
   ```
   note가 남아 있는데 **worktree에 마커가 하나도 없으면** 안내만 출력하고
   **막지 않는다.** 막을 수 있는 것은 마커 게이트뿐이며(P2), stash를 처리할
   때까지 무관한 commit을 전부 막는 것은 3번과 같은 이유로 엉뚱한 행동을 벌하는
   일이다. 이 상태를 실제로 거절하는 곳은 push 경계다. 문장은 §해소와 완료의
   보고 분류를 따른다.
   ```text
   sanho: the sync from <a> to <b> is not completed; no commit has changed the files it conflicted on
   sanho: the sync from <a> to <b> is not completed — the resolution is committed, and only 'sanho sync --continue' records it
   sanho: the sync from <a> to <b> is not completed — no resolution has been committed yet
   ```
   **예외는 없다. 창 안의 모든 commit이 이 줄을 받는다.** 예전에는 "지금
   만들어지는 commit이 곧 해소로 보이면" 침묵했는데, 그 침묵이 3차 재현의
   전반부다. 사용자가 sync가 끝났다고 믿기 가장 쉬운 바로 그 순간에 아무 신호도
   주지 않았다.

   note를 읽지 못해도 마찬가지로 막지 않는다. Sanho 자신이 읽지 못하는 파일은
   Sanho의 문제이고, 그것으로 commit을 막는 것이 v0.2가 제거한 Critical C1의
   실패 등급이다. abort 안내만 출력하고 통과시킨다.
3. **이 commit이 새로 넣거나 바꾸는** staged docs에 마커가 있으면 commit을
   막는다. 범위는 index 전체가 아니라 `HEAD` 대비 staged diff다(unborn HEAD면
   empty tree 대비, 즉 전부 추가).
   ```text
   sanho: staged docs contain conflict markers:
     docs/api.md
   Resolve them, then 'git add' the files and commit again.
   ```
   범위를 좁힌 것은 게이트의 대상이 "이 commit이 들여오는 것"이기 때문이다.
   이미 `HEAD`에 있는 마커는 다른 경로로 들어온 것이고(`--no-verify` commit,
   checkout, v0.1 시절 commit), 손대지도 않은 파일을 고칠 때까지 무관한
   commit을 전부 막는 게이트는 엉뚱한 행동을 벌한다. 게시되는 tree 전체를
   지키는 일은 §게시 계약의 마커 게이트가 맡는다.
4. 신선도 경고. base와 마지막 fetch로 얻은 canonical head를 비교한다. 여기서
   fetch하지 않는다.
   - 최신이면 **아무것도 출력하지 않는다**. 침묵이 성공 신호다.
   - behind이고 병합이 clean으로 예측되면
     `sanho: docs base is N commits behind — 'sanho sync' will merge cleanly`
   - 충돌이 예측되면
     `sanho: docs base is N commits behind — 'sanho sync' will report conflicts in <files>; syncing sooner keeps them small`
   - 예측을 계산하지 못하면
     `sanho: docs base is N commits behind — run 'sanho sync' to reconcile`
   - 마지막 fetch가 24시간보다 오래됐으면 뒤에
     ` (canonical last checked <age> ago)`를 덧붙인다.
   - **sync note가 있으면 이 경고 전체를 생략한다.** 창 동안 base는 의도적으로
     직전 값에 머무르므로 workspace는 실제로 계속 behind이고, 매 commit마다
     같은 사실을 덜 유용하게 반복하게 된다. 게다가 그 경고가 안내하는
     `sanho sync`는 note가 있는 동안 거절된다. 한 상태에 한 줄이면 된다.
5. 신선도 상태는 어떤 경우에도 exit 0이다. clone이 없거나, base를 읽지
   못하거나, canonical head가 없으면 조용히 통과한다. 막을 수 있는 것은 마커
   게이트뿐이다.

#### clone 생성은 원자적이다

clone은 **형제 staging 디렉터리**(`sanho/canonical.building-XXXX`, 같은 부모이므로
같은 파일시스템)에서 `init --bare` → `remote add` → 최초 fetch → 게시 branch 확정까지
전부 마친 뒤, `rename` 한 번으로 제자리에 들어온다. 실패하면 staging을 지우고,
성공하면 rename이 이미 그것을 소비했다. 최종 경로는 **없거나, 완성된 clone**이며
그 사이 상태가 없다.

생성 자체는 `<git-common-dir>/sanho-clone.lock`으로 직렬화하고 락 안에서 존재를
다시 확인한다(기다린 쪽은 승자의 clone을 채택한다). 하지만 락만으로는 부족하다 —
**관찰**이 동기화되지 않기 때문이다. 첫 `stat`은 락 밖에서 일어나므로, 예전의
제자리 생성에서는 두 번째 프로세스가 승자가 막 만든 디렉터리를 보고 "이미 있다"
분기로 들어갈 수 있었다.

그때 열린 것은 반쯤 만들어진 clone보다 나빴다. clone은 애플리케이션 저장소의 git
디렉터리 **안에** 있으므로, 아직 저장소가 아닌 디렉터리에서 `git rev-parse`를 돌리면
**위로 올라가** 애플리케이션 저장소를 찾는다. 그래서 `reconcileExisting`이 앱 자신의
`remote.origin.url`을 읽어 canonical URL과 다르다고 판단하고 `git remote set-url`을
실행했다 — 그 사이 승자의 `init --bare`가 끝나 있어 `No such remote 'origin'`으로
실패했고, 패자의 정리가 승자의 clone을 지웠다.

그래서 두 곳을 함께 막는다. 생성이 원자적이라 부분 상태가 관찰될 수 없고,
`Open`은 **디렉터리가 자기 자신의 저장소일 때만** 성공한다(`rev-parse
--absolute-git-dir`이 그 디렉터리 자신을 가리켜야 한다). 그리고
`reconcileExisting`은 `add`와 `set-url` 중 무엇을 할지 config 값이 아니라 `git
remote` 목록에서 직접 판정한다 — 값의 존재는 그 config가 쓰려는 저장소의 것일
때만 remote의 존재를 뜻하기 때문이다.

sync 예측은 애플리케이션 저장소가 아니라 private clone에서 계산한다. 세 tree가
한 object database에 있어야 하는데, 애플리케이션 저장소 쪽에서 하면 읽기 전용
검사가 매 commit마다 canonical object를 사용자 저장소에 밀어넣고 `FETCH_HEAD`를
덮어쓰게 된다. 대신 애플리케이션 tip을 Sanho가 소유하고 `sanho clean`이 지우는
private clone으로 가져온다.

## 상태 파일

| 파일 | 위치 | 권한 | 수명과 내용 |
|---|---|---|---|
| `.sanho.json` | 작업공간 root | `0644` | v2 workspace 설정. `schema_version`, `workspace_id`, `project`, `docs_repo_url`, `actor_email`, `docs_dir`. `socket_path`는 없다. **v0.1 판정 기준은 `socket_path`의 존재다.** `schema_version`도 `socket_path`도 없는 파일은 v0.1이 아니라 손상이며(`ErrConfigCorrupt`), 파일 이름을 말하며 거절한다. v0.1로 오판하면 `sanho migrate`가 없는 필드로부터 그 파일을 다시 쓰게 된다. |
| `.sanho_base.json` | 작업공간 root | `0644` | base 포인터. `{"version": 2, "commit": "<oid>", "tree": "<oid>"}`. tree는 비어 있을 수 있다(legacy 채택). 손상되면 fail-closed로 오류다. |
| `.sanho_docs_hash` | 작업공간 root | — | v0.1 legacy. 읽기 전용 호환 입력이며 Sanho는 쓰지 않는다. `.sanho_base.json`이 없을 때만 한 줄 OID로 읽는다. |
| `sanho/sync.json` | git-dir | `0644` (디렉터리 `0700`) | 충돌 sync 진행 중에만 존재. `{prev_base, target, started_at, entry_head, entry_docs_tree, merged_tree, conflicts}`. `entry_head`·`entry_docs_tree`는 마커를 쓸 당시의 `HEAD`와 그 docs tree, `merged_tree`는 충돌 병합이 만든 docs tree, `conflicts`는 병합이 해결하지 못한 저장소 기준 경로들이다. `entry_head`는 `--continue`의 네 번째 전제(같은 이력 위인가)를 판정하고, `merged_tree`는 완료가 병합 결과에서 얼마나 벗어났는지 보고한다. 셋은 끝나지 않은 sync가 어떤 모양인지 **보고**하기 위한 것이며, 무엇도 완료시키지 않는다. `target`은 창 동안 **대상을 담고 있는 유일한 기록**이다 — base 파일은 `sanho sync --continue`가 완료할 때까지 직전 값에 머무른다. 파싱에 실패해도 **존재는 참**이므로 게이트는 계속 거절하고 `sanho sync --abort`는 성립한다(그리고 base 파일을 지운다). |
| `sanho/canonical/` | git-common-dir | `0700` | private bare clone. linked worktree는 공통 dir을 공유하므로 하나를 함께 쓴다. **생성은 원자적이다**: 아래 참조. |
| `sanho/canonical/sanho-last-fetch` | 위와 동일 | `0644` | 마지막 성공 fetch 시각(RFC3339Nano 한 줄). |
| `~/.sanho/state.json` | sanho home | `0600` | 레지스트리. `{"version":2,"projects":{...},"workspaces":{...}}`. |
| `~/.sanho/state.json.bak` | sanho home | `0600` | 매 갱신마다 같은 바이트로 함께 쓰는 백업. |
| `~/.sanho/state.lock` | sanho home | `0600` | flock 대상 파일. |

sanho home은 `SANHO_HOME`(절대 경로여야 한다)이 있으면 그것, 없으면
`~/.sanho`이며 디렉터리 자체는 `0700`으로 강제한다. 이미 더 느슨한 권한으로
존재하던 디렉터리도 열 때 `0700`으로 조인다.

base 파일의 불변식은 둘이고, 둘째가 첫째의 실패 방향을 정한다.

1. **base 파일은 언제나 "worktree docs가 어느 canonical 상태에서 파생됐는가"에
   답한다.**
2. **base는 worktree docs보다 앞설 수 없다. 둘 다 확정할 수 없으면 더 오래된
   값을 택한다**(§해소와 완료 — base "앞서지 않음" 불변식). 그래서
   `--continue`는 note를 먼저 지우고 base를 나중에 쓰며, 읽을 수 없는 note의
   abort는 base를 지우고, 재유도·`doctor --fix`·`init`은 note가 있는 동안
   물러난다.

첫째를 다시 말하면, 따라서 docs worktree를 바꾸는
동작(`pull`, `sync`, 체크아웃 재유도)만 base를 옮긴다. 유일한 예외는 게시 후
전진 규칙인데, worktree tree와 게시된 tree가 같을 때만 옮기므로 불변식을
그대로 지킨다. commit은 base를 옮기지 않는다. 그래서 v0.2에는 `post-commit`
hook이 없다.

**충돌한 sync는 그 불변식을 지키기 위해 base를 미룬다.** 마커가 worktree에
있는 동안 docs는 여전히 병합 이전 상태에서 파생돼 있으므로, base는 직전 값에
머무르는 것이 정직하다. 대상은 note가 들고 있다가 해소가 확정되는 순간
채택된다. 창 동안 base를 쓰는 주체는 없다 — 체크아웃 재유도는 물러나고,
`doctor`의 `base-derivation` 검사도 침묵하며, `sanho sync`와 `sanho pull`은
note가 있으면 거절한다. 유일한 예외가 abort이고, 그것은 창을 닫는 동작이다.

레지스트리 스키마는 다음과 같다.

```json
{ "version": 2,
  "projects":   { "<name>": { "docs_repo_url": "<url>" } },
  "workspaces": { "<project>:<abs-path>": {
       "project": "…", "local_path": "…",
       "base_commit": "…", "base_tree": "…",
       "actor_email": "…", "last_updated_at": "RFC3339" } } }
```

레지스트리는 **관찰용**이다. 게시 정확성은 여기에 전혀 의존하지 않는다.
그래서 갱신 실패는 성공한 작업을 되돌리지 않고 조용히 무시된다.

모든 상태 쓰기는 `fsx.WriteFileAtomic`을 거친다. 같은 디렉터리의 고유
임시 파일 → chmod → write → 파일 fsync → rename → 디렉터리 fsync 순서이며,
부분적으로 쓰인 대상 파일을 남기지 않는다. 대상 디렉터리는 호출자가 미리
만들어야 한다. 이 경로를 쓰는 대상은 레지스트리와 백업, base 파일, workspace
설정, sync note, hook 파일, fetch 시각 표식이다.

### linked worktree

`.sanho.json`은 gitignore 대상이므로 `git worktree add`가 만든 checkout에는
따라오지 않는다. 그래서 linked worktree에서는 hook이 작업공간을 찾지 못하고
아무 일도 하지 않았다 — 게이트도, stamp도, 게시도, 메시지조차 없었다. 설치되어
있으면서 완전히 불활성인 도구는 없는 것보다 나쁘다.

이제 설정이 없는 디렉터리는 git에게 **main worktree**를 물어(`git worktree list
--porcelain`의 첫 레코드) 그곳의 `.sanho.json`을 읽는다. 나머지 배치는 그대로
worktree 단위다.

| 대상 | 어디에 매인가 | 이유 |
|---|---|---|
| `.sanho.json` | main worktree | gitignore 때문에 linked worktree에는 없다. |
| `.sanho_base.json` | **각 worktree** | base는 "이 worktree의 docs가 어느 canonical 상태에서 왔는가"이고, worktree마다 다른 branch가 나와 있으므로 답도 다르다. `git worktree add` 직후 post-checkout hook이 이력에서 새로 유도한다. |
| sync note | 각 worktree(`.git`의 worktree 전용 디렉터리) | 진행 중인 작업은 그 worktree의 것이다. |
| canonical clone | common dir | 원래부터 공유였다(§5.2). |
| registry 항목 | main worktree 경로로 한 줄 | registry가 답하는 질문은 "이 project의 checkout이 어디에 있는가"이고, 한 clone의 worktree 다섯 개는 그 질문의 기준으로 checkout 하나다. |

## 동시성 계약

동시성 통제는 두 겹이고, 겹치지 않는다.

**게시 직렬화는 git이 한다.** canonical에 대한 push는 compare-and-swap이며
조건 검사는 원격 서버가 수행한다. 프로세스 안 mutex와 달리 프로세스는 물론
**머신을 가로질러** 동작한다. 경합에서 지면 refetch 후 case 분석을 처음부터
다시 하고, 최대 3회 시도한다. 게시된 모든 commit은 expectedOld를 parent로
가지므로, lease가 통과했다면 그 push는 fast-forward이기도 하다.

**로컬 레지스트리는 flock이 지킨다.** `~/.sanho/state.lock`에 배타
`flock`을 걸고 read-modify-write 후 해제한다. 읽기(`status`, `state`)도 같은
배타 잠금을 쓴다. 레지스트리는 짧게 사는 CLI 호출이 갱신하므로 공유/배타
구분의 실익이 없고, 잠금 코드와 그 실패 경로를 한 곳에 모으는 쪽이 낫다.
대기는 비차단 시도를 20 ms 간격으로 폴링하는 방식이고, 5초 또는 context
deadline 중 이른 쪽에서 포기한다.

```text
another sanho process holds <lock-path>; retry in a moment
```

primary가 깨지면 `.bak`에서 복구하고 primary를 즉시 되살린다. 둘 다 읽을 수
없으면 **빈 상태로 시작하지 않고** 두 경로를 모두 이름지어 오류를 낸다.

**병합은 flock이 직렬화한다.** 병합 임시 ref(`refs/sanho-ours`,
`refs/sanho-upstream`)는 §5.4가 마커 라벨을 고정하기 때문에 고정 이름이고,
고정 이름은 잠금 없이는 안전하지 않다. 한 ref 저장소에 동시에 두 병합이 오는
상황은 실제로 셋이다.

- pre-commit 신선도 예측이 private clone에서 병합하는 동안 같은 clone으로
  `git push`가 게시할 때,
- linked worktree 둘이 clone과 애플리케이션 ref 저장소를 함께 나눠 쓸 때
  (`refs/sanho-ours`는 worktree 전용 ref 네임스페이스가 아니라 공용 ref다),
- 한 checkout에서 `sanho sync` 두 개가 겹칠 때.

따라서 ref 기록 → `merge-tree` → ref 삭제 구간 전체를
`<git-common-dir>/sanho-merge.lock`에 대한 배타 flock으로 감싼다. common dir을
쓰는 것이 핵심이다. 그래야 linked worktree들이 같은 파일에서 경합한다. 잠금
파일은 언제나 `.git` 안에 둔다. 사용자 worktree에 Sanho가 만든 untracked 파일을
남기지 않기 위해서다.

복구도 계약의 일부다. 죽은 병합은 ref를 남기므로, 잠금을 얻은 뒤 가장 먼저
하는 일이 남은 ref를 지우는 것이다. 남아 있다고 거절하면 작업공간이 영구히
막힌다. 병합을 아예 수행하지 못한 경우(잠긴 ref 저장소, 깨진 clone)는 충돌과
구별되는 `ErrMergeFailed`이고, push 경로는 raw 오류 체인 대신 §5.9 형태의
메시지를 출력한다.

## git 실행 정책

모든 git 호출은 `internal/infra/gitx`의 Runner를 거친다.

- argv 전용이다. 셸을 경유하지 않는다.
- `GIT_TERMINAL_PROMPT=0`을 항상 설정한다.
- network 작업에는
  `GIT_SSH_COMMAND="ssh -o BatchMode=yes -o ConnectTimeout=10"`을 추가한다.
  자격 증명이 없으면 프롬프트 대신 즉시 실패한다.
- 정책 환경 변수는 상속된 환경 뒤에 붙이므로, 사용자의 기존
  `GIT_SSH_COMMAND` 설정을 덮어쓴다.
- **저장소를 지정하는 환경 변수는 상속하지 않는다.** `GIT_DIR`,
  `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR`,
  `GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`,
  `GIT_PREFIX`, `GIT_CEILING_DIRECTORIES`, `GIT_QUARANTINE_PATH`,
  `GIT_NAMESPACE`, `GIT_GRAFT_FILE`을 자식 환경에서 제거한다. Runner는 대상
  저장소를 언제나 `dir`로 명시하는데, 상속된 `GIT_DIR`은 명시적인 `-C`조차
  이긴다(git 2.50.1 확인). git은 **linked worktree**에서 hook을 실행할 때
  절대 경로 `GIT_DIR`을 반드시 내보내므로, 씻지 않으면 hook 안에서 낸 모든
  git 명령이 — private canonical clone을 향한 것까지 — 애플리케이션 저장소에
  대해 실행됐다. 실제로 `reconcileExisting`의 `git remote set-url origin`이
  사용자의 `origin`을 docs clone URL로 영구 재작성했다.
- **한 변수만 명시적으로 되돌려 받는다.** `GIT_INDEX_FILE`은 위 목록에 함께
  있고, 애플리케이션 저장소용 Runner만 `WithInheritedIndexFile()`로 자기
  프로세스의 값을 **명시적으로** 전달한다. `git commit -- docs`는 부분
  commit이라 git이 임시 index를 만들어 그 hook들의 `GIT_INDEX_FILE`을 거기로
  가리키고, §5.1 stamp와 §5.6 staged 마커 게이트가 바로 그것을 읽어야 하기
  때문이다. 그냥 씻기만 했을 때는 모든 `sanho sync` commit이 조용히
  `docs-base` trailer를 잃었다(stamp는 설계상 fail-open이라 아무 신호도
  없었다). 의존은 그대로이고, 달라진 것은 그것이 **한 호출 지점에 적혀
  있다**는 점이다 — clone을 향한 명령을 포함해 다른 모든 Runner는 이 변수를
  받지 않는다.
- 기본 timeout은 60초, canonical fetch/push는 120초다.
- timeout이나 취소로 죽일 때는 프로세스 그룹째 종료하고 최대 3초 동안
  파이프 정리를 기다린다.
- `Run`은 비정상 종료를 오류로 만들고, `RunExit`은 exit code를 값으로
  돌려준다. `merge-tree`의 1, `merge-base --is-ancestor`의 1처럼 의미 있는
  비0 코드를 읽어야 하는 곳에서 `RunExit`을 쓴다.

## hook 6종

| hook | 설치 라인 | 역할 | 실패 정책 |
|---|---|---|---|
| `pre-commit` | `sanho hook pre-commit` | staged 마커 게이트 + 로컬 신선도 경고 | 마커 게이트만 차단, 그 외 항상 통과 |
| `commit-msg` | `sanho hook commit-msg "$1"` | `docs-base` / `docs-base-tree` stamp | 절대 차단하지 않음 |
| `pre-push` | `sanho hook pre-push "$@"` | 게시 + 마커 게이트 + sync 게이트 | 유일한 fail-closed hook |
| `post-checkout` | `sanho hook post-checkout "$@"` | base 재유도 (파일 checkout이면 stand-down) | 항상 exit 0 |
| `post-merge` | `sanho hook post-merge` | base 재유도 | 항상 exit 0 |
| `post-rewrite` | `sanho hook post-rewrite "$@"` | base 재유도 | 항상 exit 0 |

`post-commit`은 없다. commit은 base를 옮기지 않고, 레지스트리 갱신은
sync/pull/게시에 얹혀 있기 때문이다.

hook 설치와 제거는 **정확한 줄 일치**로만 판단한다. 부분 문자열 검사는 어디에도
없다. 그래서 `sanho hook pre-push`와 `sanho hook pre-push "$@"`가 서로를
가리지 않고 각각 독립적으로 다뤄진다. hook 파일은 사용자 스크립트이고 Sanho는
손님이므로:

- 남의 줄은 설치와 제거 양쪽에서 원문 그대로 보존한다.
- 스크립트가 `exit`으로 끝나면 그 앞에 삽입한다. 뒤에 붙이면 실행되지 않는다.
- 파일이 없으면 `#!/bin/sh` + 한 줄로 새로 만들고 `0755`를 준다.
- 이미 있는 파일에는 owner execute 비트만 더한다. group/world까지 넓히지 않는다.
- 제거 후 **Sanho 자신이 쓴 `#!/bin/sh` 한 줄만** 남은 파일은 삭제한다. 주석은
  내용이므로 남긴다 — hook의 용도를 적어 둔 머리말이나 되살리려고 주석 처리해
  둔 줄까지 지우는 것은 사용자 파일을 지우는 일이다(F-L2).
- 제거 대상에는 v0.1의 7종 라인(구형 `sanho hook pre-push` 무인자 형태와
  `post-commit` 포함)도 들어간다.

`sanho doctor`의 hooks 검사는 설치 여부, 중복 설치 횟수, 실행 비트,
남아 있는 v0.1 라인을 각각 보고한다.

## v0.1 workspace 강등 계약

hook 라인은 `sanho`를 이름으로 호출한다. 따라서 v0.2 binary를 설치하는 순간
v0.1 workspace의 hook도 새 binary로 들어온다. v0.2는 반쯤 동작하는 대신
안전하게 강등한다.

- `pre-commit`, `commit-msg`, `post-*`: 한 줄 힌트만 출력하고 exit 0.
- `pre-push`: fail-closed로 거절.
- 읽기·변경 명령(`status`, `sync`, `pull`, `doctor` 등): exit 1로 거절.
- `sanho state`: exit 0. 이 명령이 읽는 것은 workspace가 아니라
  `~/.sanho/state.json`이고, registry는 어느 workspace에도 속하지 않는다.
  v0.1 workspace 안에서 실행되었다는 사실은 출력 범위를 project로 좁힐 수
  없다는 뜻일 뿐이므로, 전체 registry를 그대로 보여주고 성공한다. 거절하면
  "다른 checkout들이 어디 있는가"라는, migrate 전에 특히 답이 필요한 질문을
  막게 된다.
- `sanho clean`: v0.1 workspace에서도 동작한다. 제거는 v0.1 상태를 해석하지
  않고 할 수 있는 유일한 일이고, hook 제거기가 legacy 라인을 알고 있다.
- `sanho migrate`: 이 상태에서 성공하는 유일한 명령이다.

```text
sanho: this workspace uses the v0.1 layout; run 'sanho migrate'
```

push 경계가 자연스러운 migration 촉구 지점이 되고, 그동안 commit은 한 번도
막히지 않는다.

## 안내 폐쇄(guidance closure) 원칙

**Sanho가 출력하는 모든 다음 단계 명령은, 그 명령이 출력된 상태에서 실제로
성공해야 한다.** 성공할 수 있는 명령이 없으면 명령을 출력하지 않고 "manual
intervention required"와 진단 정보를 출력한다. 실패할 것이 뻔한 명령을 안내하는
것보다 낫기 때문이다.

이 원칙이 구현에 남긴 흔적은 다음과 같다.

- 다음 단계 명령을 담은 모든 문자열은 `internal/interface/cli/messages.go`의
  메시지 카탈로그 한 곳에 있다. 호출 지점에서 문자열을 조립하지 않는다.
- 같은 파일이 각 안내를 `{ID, Source, Scenario, Sample, Match, NextCommands}`
  항목으로 열거하는 `Catalog`를 노출한다. 열거 가능해야 (상태, 메시지) 쌍을
  표로 검사할 수 있기 때문이다.
- 패키지 단위 테스트가 `messages.go`를 **소스로 파싱해** 다음 단계 명령을
  담은 문자열 리터럴을 찾아내고, 카탈로그에 등록되지 않은 안내가 있으면
  빌드를 실패시킨다. 안내를 추가하면서 등록을 빠뜨릴 수 없다.
- 같은 스캔이 `internal/interface/cli`의 **모든** 비테스트 파일과
  `internal/usecase/docsync`, `internal/usecase/publish`의 오류 sentinel까지
  훑는다. `messages.go` 바깥에서 명령을 이름짓는 문자열은 그 자체로 실패다.
  카탈로그가 열거하지 못하는 안내는 closure 스위트가 볼 수 없는 안내이고,
  실제로 `sanho pull`의 안내는 그렇게 해서 자신이 출력되는 상태에서 실패하는
  명령을 이름짓게 되었다. 따라서 use case의 sentinel은 사실만 말하고
  (`local docs have changes a fast-forward cannot carry`), 다음 단계는 CLI가
  카탈로그 항목으로 렌더링한다.
  cobra의 `Use`/`Short`/`Long`/`Example`은 스캔에서 제외한다. 명령을 설명하는
  설명서이지, 특정 상태에서 출력되어 실행 가능해야 하는 메시지가 아니다.
- `test/cli/e2e`의 closure 스위트가 카탈로그 항목마다 실제 작업공간을 그
  상태로 만들고, 메시지가 실제로 출력되는지 확인한 다음, 안내된 명령을 그
  상태에서 그대로 실행해 성공을 요구한다. 명령마다 새 작업공간을 쓴다.
  "그 상태에서"가 주장의 전부이므로, 앞선 명령이 상태를 바꾼 뒤에 다음
  명령을 실행하면 더 약한 것만 증명된다.
- 안내가 ref를 지목할 때는 해석된 게시 branch 이름을 쓴다. private clone에는
  `refs/remotes/origin/HEAD`가 없으므로 HEAD를 지목하는 조회 명령은 그것이
  출력되는 바로 그 상태에서 실패한다.
- `sanho sync --abort`는 구조적으로 실패할 수 없다.
- 게시가 `no_base`로 거절할 때 `sanho sync`가 실제로 성공하도록,
  base 없는 sync는 empty tree를 병합 base로 삼는다.
- rewrite 안내는 `docs-base-tree` 검색을 다시 수행해 **실재하는 commit만**
  이름에 넣는다. 후보가 없으면 명령 대신 후보 목록 조회 방법을 보여준다.
- 실행할 수 없는 조합은 거절한다. `--abort`·`--continue`·`--rebase-onto`는 서로
  배타적이며, 함께 주면 조합을 이름지어 거절한다(우선순위로 조용히 해결하지
  않는다).
- 두 단계가 필요한 안내는 두 단계로 쓴다. `sanho pull` 거절이 그렇다.
  `sanho sync`도 clean docs를 요구하므로 "commit or stash your docs changes,
  **then** run `sanho sync`"라고 적고, closure fixture가 그 두 단계를 순서대로
  수행한다. 첫 동사만 적으면 그 명령은 출력된 바로 그 상태에서 실패한다.
- 순서가 계약의 일부인 안내는 카탈로그가 순서를 **선언**한다. 항목의
  `Prerequisites`가 "이 명령보다 먼저 실행되어야 하는 명령들"을 적고, closure
  스위트가 같은 작업공간에서 그것들을 순서대로 실행하며 각각의 성공을 요구한다.
  충돌 템플릿의 `git add docs/ && git commit` → `sanho sync --continue`가 그
  예다. 두 번째 명령만 따로 실행해 보는 것은 사용자가 안내받은 적 없는 절차를
  증명하는 일이다.
- 이름 붙일 수 있는 명령이 없으면 붙이지 않는다. `--rebase-onto`를 건강한
  base의 조상에 겨눈 경우, 병합을 아예 수행하지 못한 경우, 크기 한계를 넘은
  텍스트 문서의 경우가 그렇다. 원인과 사실을 말하고 끝낸다.

메시지 위생 규칙도 함께 강제한다. CLI 출력은 영어 전용이고, OID는 12자로
줄이며, raw Go 오류 체인을 사용자에게 그대로 보이지 않고 원인 줄 + 조치 줄로
감싼다. 캐시된 결과를 제공하는 줄은 언제나 데이터 나이를 함께 말한다.

## 종료 코드

| 코드 | 의미 |
|---|---|
| 0 | 성공. 충돌 sync도 여기에 속한다. |
| 1 | 사용자가 조치할 수 있는 상태. 메시지가 그 상태를 이름짓는다. |
| 2 | Sanho 내부 결함. panic은 스택과 함께 여기로 분류된다. |

`sanho doctor`는 경고를 찾아도 0으로 끝난다. 문제를 찾을 때마다 실패하는
진단 명령은 문제 조사에 쓸 수 없기 때문이다. 검사 자체를 실행할 수 없을 때만
1이다.

## 참고

- 명령별 JSON 스키마와 자동화 규범: [CLI JSON 출력](cli-json.md)
- 일상 흐름과 장애 대응: [운영 가이드](operations.md)
- 복구 절차: [복구 가이드](recovery.md)
- 설치·업그레이드·제거: [배포 규칙](deployment.md)
- 릴리스 전 수동 검증: [hands-on 테스트](hands-on-testing.md)
