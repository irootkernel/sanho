# Sanho 배포 규칙

실제 원격 저장소, branch 보호 규칙, 여러 머신을 포함한 설치·업그레이드 검증은
[hands-on 테스트](hands-on-testing.md)를 따른다.

## 지원 범위

Sanho는 macOS와 Linux에서 사용자 단위로 설치한다. **실행 파일은 `sanho` CLI
하나뿐이다.** daemon도, service 등록도, container image도, 운영체제 package도,
자동 installer도 없다.

상주 프로세스가 없으므로 CI runner, container, 비대화형 SSH 세션에서도 개발
머신과 동일하게 동작한다. 로그인 세션, launchd/systemd, socket 경로 길이,
service 소유권 같은 개념 자체가 사라졌다.

## 요구사항

| 항목 | 요구 | 비고 |
|---|---|---|
| Go | 1.25 이상 | **설치할 때만** 필요하다. 설치된 binary 실행에는 필요 없다. |
| Git | 설치되어 있을 것 | 버전을 강제하지 않는다. 아래 정책 참고. |
| OS | macOS 또는 Linux | `flock`을 사용한다. |
| 자격 증명 | docs 저장소를 읽고 쓸 수 있는 SSH 등 | 비대화형이어야 한다. 아래 참고. |

Node.js와 npm은 필요하지 않다. 런타임 의존성은 `cobra` 하나뿐이다.

### git 버전 정책 — 강제하지 않는다

`sanho init`은 git 버전을 검사하지 않고, `sanho doctor`는 감지한 버전을 정보로만
보고한다.

```text
[ok  ] git              git version 2.44.0 (no minimum is enforced; merges need git 2.38 or newer)
```

병합 경로는 `git merge-tree --write-tree`를 사용하며 이 옵션은 실무상 git 2.38
이상이 필요하다. 더 낮은 git에서는 그 명령이 git 자신의 명확한 오류로 실패하고,
Sanho는 그 오류를 그대로 표면에 드러낸다. 미리 대체 구현을 만들어 두지 않는
쪽을 택했다. 실제 요구가 생기면 버그 리포트로 다루면 되고, 두 개의 살아 있는
병합 구현을 유지하는 편이 더 위험하기 때문이다.

병합을 쓰지 않는 경로(commit 감지, `status`, fast-forward 게시)는 더 낮은
git에서도 동작한다.

### 비대화형 자격 증명

Sanho는 git을 argv로만 실행하고 프롬프트를 띄우지 않는다. 모든 network 작업에
다음을 강제한다.

```text
GIT_TERMINAL_PROMPT=0
GIT_SSH_COMMAND="ssh -o BatchMode=yes -o ConnectTimeout=10"
```

따라서 passphrase가 걸린 key는 ssh-agent에 미리 올려야 하며, 그렇지 않으면
기다리지 않고 즉시 실패한다. 이 설정은 사용자의 기존 `GIT_SSH_COMMAND`보다
우선한다.

## 설치

release 설치는 재현 가능하도록 버전을 명시한다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.0
sanho version
```

Go는 실행 파일을 `GOBIN`에, `GOBIN`이 비어 있으면 `$(go env GOPATH)/bin`에
설치한다. 대화형 shell에서 명령을 실행하려면 그 디렉터리가 `PATH`에 있어야
한다. 설치되는 Git hook은 이 binary의 canonical 절대 경로를 기록하므로 hook
실행 환경의 `PATH`에는 의존하지 않는다.

```bash
command -v sanho
```

checkout에서 설치하려면 다음을 쓴다.

```bash
make cli-build     # bin/sanho
make cli-install   # go install ./cmd/sanho
make install       # cli-install의 별칭
```

## 온보딩

v0.1은 daemon 소유권이 절차의 절반이었다. v0.2에서 그 부분이 전부 사라졌다.

| | v0.1 | v0.2 |
|---|---|---|
| 1 | `sanho`와 `sanhod` 두 binary 설치 | `sanho` 한 binary 설치 |
| 2 | `command -v`로 절대 경로 확인 | 애플리케이션 저장소에서 `sanho init` |
| 3 | launchd plist 또는 systemd unit 작성 | — |
| 4 | service bootstrap / enable | — |
| 5 | Unix socket으로 healthz 확인 | — |
| 6 | `sanho init`(비기본 socket이면 `--socket`) | — |

**6단계에서 2단계로 줄었다.** 그리고 남은 두 단계 중 어느 것도 사용자가
서비스를 소유하거나, 재시작 정책을 정하거나, 로그를 보존하도록 요구하지 않는다.

애플리케이션 Git 저장소의 **최상위**에서 초기화한다.

```bash
cd /path/to/app
sanho init \
  --project example \
  --docs-repo-url git@github.com:example/example-docs.git
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--project` | (필수) | 프로젝트 이름. 레지스트리에서 docs 저장소 URL과 묶인다. |
| `--docs-repo-url` | (필수) | canonical docs 저장소 URL. 작업공간 설정에 직접 기록된다. |
| `--docs-dir` | `docs` | 저장소 root 기준 docs 디렉터리. |
| `--actor-email` | `git config user.email` | canonical commit에 기록되는 주소. |
| `--force` | off | 기존 docs 디렉터리를 canonical 내용으로 대체한다. `-y`가 함께 필요하다. |
| `-y`, `--yes` | off | 파괴적 동작을 확인 없이 진행한다. |

`sanho init`이 하는 일은 다음과 같다.

1. 레지스트리에 프로젝트를 등록한다. 이미 다른 URL로 등록돼 있으면 **작업공간
   파일을 하나도 건드리기 전에** 중단한다.
2. `.sanho.json`(v2 스키마)을 쓴다.
3. `<git-common-dir>/sanho/canonical`에 private bare clone을 만들고 fetch한다.
4. 상태에 따라 base를 정한다.
   - **fresh**: canonical에 내용이 있고 로컬 docs가 없으면 canonical docs를
     체크아웃해 index에 올리고 head를 base로 채택한다. commit은 사용자가 한다.
     ```text
     Canonical docs are staged. Commit them:  git commit -m 'docs: adopt canonical docs'
     ```
   - **bootstrap**: canonical이 비어 있으면 base를 기록하지 않는다.
     ```text
     sanho: canonical repository is empty; your first push will publish docs
     ```
   - **reuse**: 로컬 docs가 이미 있으면 저장소 이력의 provenance에서 base를
     유도하고 사용자 파일은 절대 건드리지 않는다. provenance가 없으면 거절한다.
5. 6개 hook을 설치한다. 남의 hook 내용은 원문 그대로 보존한다.
6. `.gitignore`에 `.sanho.json`, `.sanho_base.json`, `.sanho_docs_hash`,
   `.sanho_pending_fix`를 추가한다(이미 있는 줄은 건너뛴다).
7. 레지스트리에 작업공간 항목을 기록한다.

이미 초기화된 작업공간에서 다시 실행하면 거절한다.

```text
sanho: .sanho.json already exists in /path/to/app; rerun with --force to reinitialize
```

저장소 최상위가 아니면 거절한다. hook은 언제나 최상위에서 실행되므로,
다른 곳에서 초기화하면 그 hook이 자기 작업공간을 찾지 못한다.

```text
sanho: run 'sanho init' at the repository root (/path/to/app)
```

확인은 다음으로 한다.

```bash
sanho status
sanho doctor
```

## ~/.sanho 구조

```text
~/.sanho/                 0700
├── state.json            0600   레지스트리(프로젝트 → URL, 작업공간 관찰 상태)
├── state.json.bak        0600   매 갱신마다 같은 바이트로 함께 쓰는 백업
└── state.lock            0600   flock 대상
```

v0.1에 있던 `sanhod.sock`과 `docs_repos/`는 없다. canonical clone은 sanho home이
아니라 각 작업공간 안에 있다.

```text
<app-repo>/
├── .sanho.json                              0644  작업공간 설정
├── .sanho_base.json                         0644  base 포인터
└── .git/
    ├── hooks/                                     6개 sanho 라인
    ├── sanho/sync.json                      0644  충돌 sync 중에만 존재
    └── sanho/canonical/                     0700  private bare clone
        └── sanho-last-fetch                 0644  마지막 fetch 시각
```

`sanho/canonical`은 git **common** directory 아래에 있으므로 linked worktree들이
하나를 공유한다. `sync.json`은 worktree private git directory 아래에 있으므로
worktree마다 독립이다.

sanho home 경로는 `SANHO_HOME`으로 바꿀 수 있으며 절대 경로여야 한다.
디렉터리는 열 때마다 `0700`으로 조인다. 이미 더 느슨한 권한으로 존재하던
디렉터리도 마찬가지다.

```text
sanho: SANHO_HOME must be an absolute path
```

## v0.1 → v0.2 업그레이드

canonical 저장소는 전혀 바뀌지 않는다. 같은 저장소, 같은 선형 main이고, 앞으로
만들어질 commit의 메시지 규약만 달라진다. `docs-version` trailer와
`[SANHO] Update docs` commit은 무해한 이력으로 남으며, v0.2는 옛 키를 읽을 줄
알므로 이력 rewrite도 migration commit도 필요 없다.

**머신 한 대씩 옮긴다.** 여러 머신이 잠시 서로 다른 버전으로 같은 canonical
저장소를 쓰는 것은 git이 직렬화하므로 안전하지만, 지원되는 구성이 아니라 전환
상태다.

### 1단계 — 백업 확인 (선택)

`sanho migrate`는 `~/.sanho/state.json`을 v0.1 daemon 스키마에서 v2 레지스트리
스키마로 제자리에서 덮어쓰기 전에, v0.1 원문을 `~/.sanho/state.json.v1.bak`으로
자동 보존한다. 추가 안전을 원하면 수동 사본을 하나 더 떠 둔다.

```bash
cp ~/.sanho/state.json ~/.sanho/state.json.pre-v0.2   # 선택
```

### 2단계 — v0.1 transaction 정리

**v0.1 binary로** 진행 중인 상태를 먼저 끝낸다.

```bash
sanho status                    # pull_commit 분류와 next_command 확인
sanho pull-commit --continue    # 또는 --abort / --recover
```

v0.2는 v0.1 transaction을 해석하지 않고 거절한다.

```text
sanho: a v0.1 pull-commit transaction or pending-fix state is still present; finish or abort it with the v0.1 binary, then run 'sanho migrate' again
```

### 3단계 — binary 교체

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.2.0
sanho version
```

`sanhod`는 지우지 않아도 된다. 아무것도 참조하지 않는다.

### 4단계 — 강등 상태를 이해한다

hook 라인이 `sanho`를 이름으로 호출하므로, binary를 바꾼 순간부터 v0.1
작업공간의 hook도 v0.2 binary로 들어온다. v0.2는 반쯤 동작하는 대신 안전하게
강등한다.

```text
sanho: this workspace uses the v0.1 layout; run 'sanho migrate'
```

- `git commit`은 계속 성공한다. hook이 위 힌트만 출력하고 통과한다.
- `git push`는 fail-closed로 막힌다. push 경계가 자연스러운 migration 촉구
  지점이다.
- 다른 Sanho 명령은 exit 1로 거절한다.
- `sanho clean`은 이 상태에서도 동작한다.

즉 **업그레이드 도중에 commit이 막히는 순간이 없다.**

### 5단계 — 작업공간마다 migrate

```bash
cd /path/to/app
sanho migrate
```

legacy state에 docs 저장소 URL이 없으면 명시한다.

```bash
sanho migrate --docs-repo-url git@github.com:example/example-docs.git
```

`sanho migrate`가 하는 일은 다음과 같다.

1. 이미 v2면 `sanho: already migrated`를 출력하고 끝낸다(멱등).
2. v0.1 transaction / `.sanho_pending_fix`가 있으면 거절한다.
3. `.sanho.json`을 `.sanho.json.bak`으로 복사한 뒤 v2 스키마로 교체한다
   (`docs_repo_url` 추가, `socket_path` 제거).
4. private clone을 만들고 fetch한다.
5. `.sanho_docs_hash`를 `.sanho_docs_hash.bak`으로 복사한다. 원본은 지우지 않고
   읽기 전용 호환 입력으로 남긴다. 그 값을 base로 채택하고, canonical에서 그
   commit이 아직 살아 있으면 tree까지 해석해 `.sanho_base.json`을 만든다.
6. v0.1 7종 hook 라인을 제거하고 v0.2 6종을 설치한다. 남의 줄은 보존한다.
7. `.gitignore` 항목을 보강한다.
8. 레지스트리에 프로젝트와 작업공간을 v2 스키마로 기록한다.
9. daemon 정지 방법을 **출력만 한다.** 실행하지 않는다. service 소유권은
   v0.1에서도 명시적으로 사용자 것이었다.

```text
sanho: the v0.1 daemon is no longer used. Stop and unload it yourself:
  macOS (launchd):  launchctl bootout gui/$(id -u)/xyz.rootkernel.sanho
  Linux (systemd):  systemctl --user disable --now sanhod
The 'sanhod' binary can be deleted at your leisure; nothing references it.
```

base를 찾을 수 없거나 canonical에서 사라졌으면 migration을 멈추지 않고 알린 뒤
계속한다. `sanho sync` 또는 `sanho doctor --fix`가 나중에 해결한다.

### 6단계 — daemon 정지와 확인

출력된 명령을 사용자가 직접 실행한다.

```bash
launchctl bootout gui/$(id -u)/xyz.rootkernel.sanho
# 또는
systemctl --user disable --now sanhod
rm ~/Library/LaunchAgents/xyz.rootkernel.sanho.plist
# 또는
rm ~/.config/systemd/user/sanhod.service
```

```bash
sanho status
sanho doctor
sanho state --all
```

`sanhod` binary는 언제 지워도 된다.

```bash
rm "$(go env GOPATH)/bin/sanhod"
```

롤백 절차는 [복구 가이드](recovery.md#5-migrate-롤백)에 있다.

## v0.2 → v0.2 업그레이드

```bash
go install github.com/irootkernel/sanho/cmd/sanho@vX.Y.Z
sanho version
sanho doctor
```

중지할 프로세스도, 마이그레이션할 socket도, 버전을 맞춰야 할 두 번째 binary도
없다. 상태 스키마가 바뀌는 release라면 해당 release note의 호환성 지침을 먼저
확인한다. rollback도 `go install`로 이전 버전을 다시 설치하면 된다.

hook 라인은 이름으로 호출하므로 binary 교체만으로 즉시 반영된다. hook 파일을
다시 쓸 필요는 없다. `sanho doctor`가 설치 상태를 확인해 준다.

## 제거

### 작업공간 단위

```bash
cd /path/to/app
sanho clean --dry-run     # 무엇이 지워질지 확인한다. 아무것도 바꾸지 않는다
sanho clean -y
```

제거 대상은 6개 hook 라인, `.sanho.json`, `.sanho_base.json`,
`.sanho_docs_hash`, `.sanho_pending_fix`, private clone, 레지스트리 항목이다.
docs 디렉터리까지 지우려면 `--remove-docs`를 별도로 지정한다.

`--dry-run`은 엄격하게 읽기 전용이다. 레지스트리 잠금 파일의 mtime조차
건드리지 않는다. 확인 없이는 실제 제거를 실행하지 않는다.

```text
sanho: 'sanho clean' removes this workspace's sanho state; rerun with -y to confirm, or 'sanho clean --dry-run' to preview
```

충돌 sync가 밀려 있으면 먼저 정리하도록 거절한다.

```text
sanho: a conflicted sync is in progress; complete it with 'sanho sync --continue', or undo it with 'sanho sync --abort' first
```

hook 제거는 정확한 줄 일치로만 하므로 남의 hook 내용은 그대로 남고, shebang과
주석만 남게 된 파일은 껍데기를 남기지 않고 삭제한다. v0.1 라인도 함께
제거한다.

### 프로젝트 등록 해제

```bash
sanho project delete <project>
```

작업공간이 아직 그 프로젝트를 참조하면 거절한다.

```text
sanho: project "example" still has 2 registered workspace(s) (/path/to/app); run 'sanho clean' in them, or rerun with --force
```

### 전체 제거

```bash
# 1) 각 작업공간에서
sanho clean -y

# 2) binary 삭제
rm "$(go env GOPATH)/bin/sanho"

# 3) sanho home
#    레지스트리만 들어 있고 문서 데이터는 없다. 확인 후 지운다.
rm -rf ~/.sanho
```

`~/.sanho`에는 관찰용 레지스트리만 있고 canonical clone은 각 작업공간 안에
있으므로, `sanho clean`을 마친 뒤라면 삭제해도 잃을 문서가 없다. canonical
저장소 자체는 어떤 제거 절차로도 건드리지 않는다.
