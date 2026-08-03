# CLI JSON 출력

Sanho의 사용자용 조회 명령은 자동화 도구와 AI agent가 안정적으로
결과를 읽을 수 있도록 `--json`을 지원한다.

```bash
sanho version --json
sanho status --json
sanho state --json
sanho --socket /absolute/path/to/sanhod.sock state --all --json
```

`--json`은 `version`, `status`, `state`의 로컬 옵션이다. Git hook과
`init`, `pull`, `fix`, `clean`, `project`, `workspace` 같은 변경 명령에는
적용되지 않는다. `sanho --json status`처럼 루트 옵션으로 사용할 수도
없다.

## 공통 규칙

- 성공 결과는 stdout에 compact JSON 객체 하나와 마지막 개행으로 출력한다.
- 실패 결과는 stderr에 JSON 객체 하나를 출력하고 stdout은 비운다.
- 실패 시 기존 종료 코드 정책을 유지한다.
- 배열은 값이 없어도 `[]`, 선택 필드는 값이 없으면 `null`로 출력한다.
- 사람용 출력 문장, 표, 축약 hash는 JSON에 섞지 않는다.

오류 형식은 다음과 같다.

```json
{"error":{"code":"not_in_workspace","message":"this directory is not a sanho workspace. Run 'sanho init' first"}}
```

`code`는 자동화 분기에 사용하는 안정 식별자이고, `message`는 진단용
설명이다.

공개 오류 코드는 다음과 같다.

- 입력·실행 위치: `invalid_arguments`, `invalid_socket_path`,
  `not_in_workspace`
- 로컬 상태: `invalid_workspace_config`, `docs_hash_not_found`,
  `docs_hash_read_failed`, `pending_fix_read_failed`,
  `pull_commit_state_failed`, `main_publication_state_failed`,
  `git_operation_detection_failed`
- daemon 상태: `unknown_project`, `unknown_workspace`,
  `workspace_project_mismatch`, `unknown_docs_commit`,
  `daemon_request_failed`
- 내부 실패: `internal_error`

## `version`

공개 계약은 `name`과 `version`만 포함한다.

```json
{"name":"sanho","version":"1.2.3"}
```

commit과 build date는 기존 사람용 `sanho version` 출력에서만 제공한다.

## `status`

`status`는 현재 작업공간과 canonical docs HEAD의 관계를 반환한다.

```json
{
  "project": "example",
  "workspace_id": "example:workspace",
  "docs_base": "0123456789abcdef",
  "docs_head": "fedcba9876543210",
  "status": "outdated",
  "docs_relation": {"status": "behind", "ahead": 0, "behind": 2},
  "pending_fix": {"exists": false, "created_at": null},
  "pull_commit": {"exists": false},
  "main_publication": {"pending": false, "sync_commits": []},
  "git_operation": {
    "active": false,
    "type": "none",
    "classification": "clear",
    "reason": "",
    "next_commands": []
  },
  "conflicts": {"scan_status": "complete", "files": []},
  "workspace_comparisons_available": true,
  "workspaces": []
}
```

`docs_relation.status`와 작업공간별 관계는 `same`, `ahead`, `behind`,
`diverged`, `unknown` 중 하나다. `ahead`와 `behind`는 항상 숫자로
제공한다. 구버전 daemon이 작업공간 비교 endpoint를 제공하지 않으면
`workspace_comparisons_available`은 `false`, `workspaces`는 `[]`가 된다.
충돌 검색을 완료하지 못하면 `conflicts.scan_status`가 `unavailable`이 된다.

진행 중인 transaction이 있으면 `pull_commit`은 `phase`, `classification`,
`reason`, `current_head`, `prepared_head`, `next_command`를 제공한다. 복구
checkpoint가 기록된 경우 `backup_head_ref`도 포함한다. 분류는 `pending`,
`completed`, `rewritten`, `recoverable_rewrite`, `ambiguous`, `corrupt` 중
하나이며, 자동화는 `next_command`를 사용자에게 그대로 제시할 수 있다.
버전 1·2의 sibling rewrite는 저장된 merged index snapshot과 현재 commit의
docs tree까지 일치해야 `recoverable_rewrite`가 된다. snapshot 누락·손상은
`corrupt`, docs 불일치는 `ambiguous`이며 두 분류 모두 transaction과
recovery ref를 유지한다. JSON 필드와 classification 값은 변경되지 않는다.

게시 대기 상태가 있으면 `main_publication.pending`은 `true`이고
`classification`, `reason`, `base_commit`, `local_main`, `remote_main`,
`sync_commits`를 제공한다. 분류는 `pending`, `blocked`, `corrupt` 중 하나다.
direct main push 뒤 로컬 상태가 잠시 남아 있어도 `status`가
remote의 `refs/heads/main`을 읽기 전용으로 확인한다. status는 로컬
`origin/main` ref나 publication metadata를 갱신·삭제하지 않는다. remote에
도달한 system commit은 status 출력에서 `pending: false`로 보이며, private
metadata는 다음 정상 guarded workflow가 멱등하게 정리한다.
게시 대기 상태가 없으면 `pending`은 `false`, `sync_commits`는 `[]`다.

`git_operation`은 항상 존재한다. operation metadata가 없으면 위 예제처럼
`active: false`, `type: "none"`, `classification: "clear"`이며 명령 배열은
`[]`다. rebase가 감지된 예는 다음과 같다.

```json
{
  "active": true,
  "type": "rebase",
  "classification": "blocked",
  "reason": "Git rebase operation metadata is present; Sanho workspace mutations are blocked",
  "next_commands": [
    "git status",
    "git rebase --continue",
    "git rebase --abort",
    "git rebase --quit"
  ]
}
```

공개 `type`은 `none`, `rebase`, `am`, `merge`, `cherry_pick`, `revert`,
`bisect`, `sequencer`, `multiple` 중 하나다. active operation은 항상
`classification: "blocked"`다. 명령 배열은 자동 실행 지시가 아니라 사용자가
`git status`를 확인한 뒤 선택할 후보이며, Sanho는 operation metadata를
자동으로 변경하지 않는다. status 반복 실행은 refs, remote-tracking refs,
index, worktree, operation metadata와 Sanho private state를 변경하지 않는다.

작업공간 항목은 기존 사람용 표와 같은 repository 라벨, workspace ID,
전체 docs hash, 현재 작업공간 및 HEAD와의 관계만 포함한다. 원본
`local_path`, 전체 repository URL과 daemon 내부 식별자는 추가로 노출하지
않는다.

## `state`

`state`는 `scope`, `project`, `docs_heads`, `workspaces`를 반환한다.
기본 명령은 현재 프로젝트만 반환하고, `--all`에서는 모든 프로젝트를
반환하며 `project`가 `null`이다.

```json
{
  "scope": "project",
  "project": "example",
  "docs_heads": {"example": "0123456789abcdef"},
  "workspaces": [
    {
      "workspace_id": "example:workspace",
      "project": "example",
      "docs_hash": "0123456789abcdef",
      "last_reported_at": null,
      "last_actor": null
    }
  ]
}
```

daemon의 `/state` 응답이 전체 프로젝트를 포함하더라도 기본
`sanho state --json` 결과는 현재 프로젝트로 필터링한다.
