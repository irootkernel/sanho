# Sanho 배포 규칙

실제 사용자 service의 재시작·업그레이드 검증은 이 문서의 책임 경계를
유지하면서 [hands-on 테스트](hands-on-testing.md)의 H10 시나리오를 따른다.

## 지원 범위

Sanho는 macOS와 Linux에서 사용자 단위로 설치하고 실행한다. 실행 파일은
`sanho` CLI와 `sanhod` daemon 두 개뿐이다. container image, 운영체제
package, 자동 service installer는 배포하지 않는다.

release 설치는 재현 가능하도록 버전을 명시한다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.3
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.3
sanho version
sanhod --version
```

Go는 실행 파일을 `GOBIN`에 설치하고, `GOBIN`이 비어 있으면
`$(go env GOPATH)/bin`을 사용한다. service를 등록하기 전에
`command -v sanho`와 `command -v sanhod`로 절대 경로를 확인한다.

## daemon 소유권

`sanhod`는 foreground 프로세스로만 동작한다. 설치 명령과 Sanho CLI는
daemon을 자동으로 시작하거나, 로그인·부팅 시 자동 실행을 등록하거나,
실패한 프로세스를 재시작하지 않는다.

항상 실행할 필요가 있다면 사용자가 직접 launchd 또는 systemd user
service를 등록한다. service 파일의 생성, 활성화, 로그 보존, 재시작 정책,
제거도 사용자의 책임이다. 시스템 전체 root service보다 로그인 사용자와
동일한 계정으로 실행하는 user service를 권장한다. 그래야 docs 저장소용
SSH 설정과 파일 소유권이 일치한다.

## 런타임 경로

기본 런타임 home은 `~/.sanho`다.

```text
~/.sanho/
├── state.json
├── state.json.bak
├── sanhod.sock
└── docs_repos/
    └── <docs_repo_id>/
```

- home과 `docs_repos/`는 `0700`으로 관리한다.
- state, backup, Unix socket은 `0600`으로 관리한다.
- 정상 종료 시 `sanhod.sock`은 제거한다.
- 시작 시 stale socket은 복구하지만, 다른 daemon이 사용 중인 socket이나
  같은 경로의 일반 파일은 덮어쓰지 않는다.

경로는 다음 순서로 결정한다.

1. daemon의 `--home`, `--socket`
2. `SANHO_HOME`, `SANHO_SOCKET`
3. `~/.sanho`, `$SANHO_HOME/sanhod.sock`

CLI의 socket 선택 순서는 전역 `--socket`, 작업공간 `.sanho.json`의
`socket_path`, `SANHO_SOCKET`, `$SANHO_HOME/sanhod.sock`이다.
`SANHO_HOME`, `SANHO_SOCKET`, `.sanho.json`의 `socket_path`는 모두 절대
경로여야 한다.

## macOS launchd 예시

다음은 형식 예시다. `/ABSOLUTE/PATH/TO/sanhod`와 사용자 home 경로를 실제
절대 경로로 바꾼 뒤 사용자가 직접
`~/Library/LaunchAgents/xyz.rootkernel.sanho.plist`에 저장한다.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>xyz.rootkernel.sanho</string>
  <key>ProgramArguments</key>
  <array>
    <string>/ABSOLUTE/PATH/TO/sanhod</string>
    <string>--home</string>
    <string>/Users/NAME/.sanho</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>StandardOutPath</key>
  <string>/Users/NAME/Library/Logs/sanhod.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/NAME/Library/Logs/sanhod.log</string>
</dict>
</plist>
```

등록과 해제도 사용자가 직접 수행한다.

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/xyz.rootkernel.sanho.plist
launchctl kickstart -k gui/$(id -u)/xyz.rootkernel.sanho
launchctl bootout gui/$(id -u)/xyz.rootkernel.sanho
```

## Linux systemd user service 예시

다음은 `~/.config/systemd/user/sanhod.service` 예시다. `ExecStart`는
`command -v sanhod`로 확인한 절대 경로로 바꾼다.

```ini
[Unit]
Description=Sanho document coordination daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/ABSOLUTE/PATH/TO/sanhod
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now sanhod.service
systemctl --user status sanhod.service
systemctl --user disable --now sanhod.service
```

로그인하지 않은 동안에도 user service를 유지할지는 운영 환경 정책에
따라 사용자가 `loginctl enable-linger "$USER"` 사용 여부를 결정한다.
Sanho가 이 설정을 변경하지 않는다.

## 상태 확인과 종료

```bash
curl --fail --unix-socket ~/.sanho/sanhod.sock http://sanho/healthz
sanho state --all
```

service manager는 종료 시 `SIGTERM`을 보내야 한다. `sanhod`는 진행 중인
HTTP 요청을 기다린 뒤 종료하고 자신이 만든 socket만 제거한다. 강제
종료는 마지막 수단으로 사용한다.

## 업그레이드와 rollback

1. 현재 `sanhod --version`과 설치 경로를 기록한다.
2. daemon을 정상 종료한다.
3. `state.json`, `state.json.bak`을 별도 위치에 보존한다.
4. 두 실행 파일을 동일한 release 버전으로 설치한다.
5. daemon을 다시 시작하고 health와 `sanho state --all`을 확인한다.

```bash
go install github.com/irootkernel/sanho/cmd/sanho@v0.1.3
go install github.com/irootkernel/sanho/cmd/sanhod@v0.1.3
```

CLI와 daemon의 release 버전을 섞어 운영하지 않는다. rollback도 daemon을
정상 종료한 뒤 두 실행 파일을 함께 이전 버전으로 다시 설치한다. state
형식 변경이 포함된 release라면 해당 release note의 호환성 지침을 먼저
확인한다.

## 제거

1. 사용자가 등록한 launchd 또는 systemd user service를 중지하고 해제한다.
2. `sanho`, `sanhod` 실행 파일을 Go binary 경로에서 제거한다.
3. 더 이상 필요하지 않은 작업공간의 Git hook과 `.sanho*` 파일은 각
   작업공간에서 `sanho clean`으로 먼저 정리한다.
4. `~/.sanho`에는 canonical docs clone과 작업공간 상태가 있으므로 자동으로
   삭제하지 않는다. 백업과 복구 필요성을 확인한 사용자가 명시적으로
   처리한다.

## Kkachi에서의 전환

Sanho는 Kkachi 설정과 런타임 데이터를 자동 마이그레이션하지 않는
clean-break release다. `.kkachi.json`, `.kkachi*`, Kkachi daemon state,
기존 service 등록을 읽거나 변환하지 않는다.

기존 Kkachi daemon과 service를 사용자가 별도로 종료·해제한 뒤 Sanho를
설치하고, 각 애플리케이션 저장소에서 `sanho init`으로 새
`.sanho.json`과 Git hook을 만든다. Kkachi 데이터는 전환 검증이 끝날
때까지 별도 백업으로 보존한다.
