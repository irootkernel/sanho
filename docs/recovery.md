# Sanho 복구 절차

이 문서는 Git operation metadata와 docs provenance 장애를 복구하는 지원 절차다.
실제 저장소에 적용하기 전에 branch 이름, local/remote OID와 `sanho status --json`
출력을 기록한다. `--no-verify`, 무조건 force push, `.git/sanho` 직접 삭제는 사용하지
않는다.

## backend 없는 `REBASE_HEAD`

`rebase-merge` 또는 `rebase-apply` 디렉터리가 있으면 실제 rebase backend가
active인 상태다. 이 경우 아래 pseudo-ref 삭제 절차를 사용하지 말고 `git status`와
Git의 continue/abort/quit 절차를 따른다.

`sanho status --json`이 `git_operation.orphaned: true`, `backend: "none"`과
`metadata_oid`를 보고한 경우에만 다음을 실행한다.

```bash
git status
git rev-parse --path-format=absolute --git-path rebase-merge
git rev-parse --path-format=absolute --git-path rebase-apply
git rev-parse --path-format=absolute --git-path REBASE_HEAD
git rev-parse --verify 'REBASE_HEAD^{commit}'
git update-ref -d REBASE_HEAD <status가-보고한-metadata_oid>
sanho status --json
```

마지막 `update-ref -d`의 OID 인자는 compare-and-delete 조건이다. marker가 확인 후
바뀌었으면 삭제가 실패하므로 상태를 처음부터 다시 조사한다. malformed marker는
유효한 조건 OID가 없으므로 Sanho가 삭제 명령을 제안하지 않는다. 파일을 `rm`으로
지우거나 Sanho가 metadata를 자동 삭제하게 하지 않는다.

## 이미 게시된 invalid branch 복구

이 절차는 docs 변경이 있지만 이를 설명하는 유효한 `docs-version` provenance 없이
이미 게시된 한 개의 선형 branch를 복구한다. merge commit, 여러 invalid commit,
branch protection 또는 공동 작업자의 새 commit이 있으면 먼저 담당자와 별도 계획을
확정한다.

아래 예시의 `BRANCH`를 실제 branch 이름으로 바꾼다. 시작 전에 원격 tip이 현재 local
tip과 같은지 확인하고, 기존 staged·unstaged·untracked 변경은 stash에 보존한다.

```bash
BRANCH=feature/name
INVALID_TIP=$(git rev-parse "refs/heads/$BRANCH")
REMOTE_TIP=$(git ls-remote --heads origin "refs/heads/$BRANCH" | awk '{print $1}')
test "$INVALID_TIP" = "$REMOTE_TIP"

STASH_REF=
if test -n "$(git status --porcelain --untracked-files=all)"; then
  git stash push --include-untracked -m "sanho provenance repair: $BRANCH"
  STASH_REF=$(git rev-parse refs/stash)
fi

git switch -c "sanho-repair/${BRANCH//\//-}" "$INVALID_TIP"
git reset --soft "$INVALID_TIP^"
git commit -C "$INVALID_TIP"
```

첫 commit 시도에서 Sanho가 `[SANHO] Update docs` commit을 만들고 같은 명령을 다시
실행하라고 요청할 수 있다. 이는 최신 canonical docs를 application `main`에
materialize하는 정상 계약이다. 표시된 그대로 `git commit -C "$INVALID_TIP"`을
한 번 더 실행한다. hook을 우회하지 않는다. 원래 commit의 완전한 tree와 복구된
commit의 tree가 같은지 확인한다.

```bash
REPAIRED_TIP=$(git rev-parse HEAD)
git diff --exit-code "$INVALID_TIP" "$REPAIRED_TIP"
git show -s --format='%H%n%P%n%B' "$REPAIRED_TIP"
sanho status --json
```

정상 hook 흐름은 canonical docs commit을 게시하고 repaired application commit에
`docs-version` trailer를 붙인다. 필요한 `[SANHO] Update docs` commit은 local
`main`에 생성되며, 다음 origin branch push 전에 Sanho가 `origin/main`을 자동
fast-forward 게시할 수 있다. commit 제목의 `[SANHO]` 문자열 자체는 생성 증거가
아니다. commit graph, parent/tree, private publication metadata와 `docs-version`을
함께 검증한다.

검증 후 원래 local branch를 repaired tip으로 옮기고, 기록해 둔 remote OID에
대해서만 lease를 건다.

```bash
git branch -f "$BRANCH" "$REPAIRED_TIP"
git switch "$BRANCH"
git push \
  --force-with-lease="refs/heads/$BRANCH:$REMOTE_TIP" \
  origin "refs/heads/$BRANCH:refs/heads/$BRANCH"

test "$(git rev-parse "refs/heads/$BRANCH")" = \
  "$(git ls-remote --heads origin "refs/heads/$BRANCH" | awk '{print $1}')"
sanho status --json
```

`--force`만 사용하는 push는 금지한다. lease 실패는 다른 게시자가 branch를
변경했다는 뜻이므로 다시 force하지 말고 새 remote tip에서 복구 계획을 재작성한다.
마지막 status의 `status`는 `up_to_date`, `head_reconciliation.pending`은 `false`여야
한다.

보존한 변경이 있었다면 repaired branch에서 다시 적용한다. 적용 성공과 staged 상태를
확인할 때까지 stash를 삭제하지 않는다.

```bash
if test -n "$STASH_REF"; then
  git stash apply --index "$STASH_REF"
fi
git status
sanho status --json
```

충돌 없이 원래 staged·unstaged·untracked 상태가 복원된 것을 확인한 뒤 해당 stash를
명시적으로 삭제할 수 있다.
