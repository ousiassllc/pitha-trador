# アーキテクチャ設計: AI のプロバイダー設定（役割ごとにプロバイダーとモデルを選ぶ。issue #1080）

`docs/architecture/overview/integrations.md` §6（Jev API連携）から分割した節。AI を呼ぶ役割ごとに「どのプロバイダーの、どのモデルか」を選ぶ層。意思決定モデル比較のトライアル（`overview/integrations-jev-trial.md`）のアームのプロバイダー選択もこの層と同じ id を使う。ChatGPT のサブスクのプロバイダー（issue #1081。`codex_chatgpt`）はこの層の対象外で、**トライアルのアームの`provider`だけ**に足した（AI の役割には選べない。`overview/integrations-codex-chatgpt.md`）。ライブに使うかどうかは、トライアルの成績を見てから別に決める。

## 位置づけ

- **プロバイダー**: `custom`（独自 API。Jev の接続＝`JEV_API_KEY`・`JEV_BASE_URL`。Sol・Opus・Luna は役割ごとの上書き`SOL_*`/`OPUS_*`/`LUNA_*`、なければ Jev）と`openrouter`（`OPENROUTER_API_KEY`・`OPENROUTER_BASE_URL`。未設定なら`https://openrouter.ai/api`）。接続情報（キー・URL）はこれまでの`secrets`のキーのままで、書き換えも移行もしない（`JEV_*`は他のアプリとも共有しているため。層は読むだけ）。
- **役割**: `jev`（売買判断。取引に直接使われる）・`sol`（改善提案）・`opus`（提案の検証）・`luna`（ニュース分類）。設定は`runtime_settings`のキー`ai.role.<role>`（JSON `{"provider":"custom|openrouter","model":"…"}`）。**何も保存しなければ、全ての役割が今までと同じ接続先・キー・モデルで呼ばれる**（`custom`・モデル指定なし）。
- **トライアルのアーム**: `jev.trial.arms`の各アームの`provider`は`openrouter`（既定の使い方。OpenRouter のキーとアームの`model`で呼ぶ）か`custom`（独自 API の URL とキー。OpenRouter 固有の`provider_only`・`data_collection`・`zdr`は付けられない）。アームのキーはこの層と同じ接続を使うので、役割ごとにキーを入れ直す必要はない。

## 解決（`internal/config/airole.Resolve`、`internal/bootstrap/airolewire`）

| 役割・プロバイダー | 接続先 | キー | モデル |
|---|---|---|---|
| jev・custom（既定） | `JEV_BASE_URL`（空なら`jev.DefaultBaseURL`） | `JEV_API_KEY` | 役割のモデル、なければ`JEV_MODEL`（空なら`jev-latest`） |
| sol / opus / luna・custom（既定） | 今まで通り: 役割の上書き（`SOL_BASE_URL`等。独自のプロトコル）、なければライブの Jev | 同左 | 同左（役割のモデルは指定できない） |
| 任意の役割・openrouter | `OPENROUTER_BASE_URL`（空なら公開エンドポイント） | `OPENROUTER_API_KEY` | 役割のモデル（必須） |

- OpenRouter を選んだ sol / opus / luna は、役割ごとの上書きを使わず、OpenRouter 向けの`jev.Client`（`/v1/systemone`）で呼ぶ。**OpenRouter のキーが無ければ、独自 API のキーに落ちず、その段は見送る**（`assist.ErrNotConfigured`）。ライブの Jev も同様に、キーが無い OpenRouter へは独自 API のキーを送らない。
- 保存した値が壊れている・読めないときは、別のプロバイダーへ動かさず**既定**で動き、起動時に警告を出し、画面に理由を出す。
- 設定の反映には**アプリの再起動が必要**（キーと同じ。`BuildServices`が起動時に 1 回解決する）。

## 画面からだけ変えられる

- 保存は Settings「外部連携・AI」の「役割ごとのプロバイダーとモデル」（`modal-ai-roles`、`GET /settings/ai-roles-panel`・`POST`/`DELETE /settings/ai-roles/:role`）だけ。**MCP の`settings_set`の許可リストに`ai.role.*`は無い**（テスト`TestAIRoleSettingsAreNotWritableFromTheMCP`）。ライブの Jev（売買判断）のプロバイダー・モデルは画面からだけ変えられ、トライアルのアームの`jev.trial.arms`も従来どおり画面だけ。
- 画面の並びは「AI のプロバイダー（独自 API・OpenRouter の接続）→ 役割ごとのモデル → そのほかの接続先」。詳しい説明は各見出しの「?」に入れる。キーの値は画面にも出さない（設定済み／未設定の表示だけ。伏せ字は既存の`secrets`の経路のまま）。

## 記録（`ai_role_history`、マイグレーション000057）

- 画面で保存・既定に戻した変更（`kind=saved`）を、設定の保存と同じトランザクションで 1 行（いつ・何から何へ・誰が（`gui`））。値が変わらない保存は行を作らない。
- アプリの起動時に、各役割が実際に使い始めた組（`kind=started`、`actor=app`）を、直前の記録と違うときだけ 1 行。保存した設定と今動いている組のずれ（再起動待ち）が分かる。
- 入るのはプロバイダー名とモデル名だけ（キー・URL は入れない）。ライブの Jev の`saved`の行は、Paper 実績 MCP の調整の実験で`JEV_MODEL`の切り替え（`jev_model_changes`。issue #885）と並ぶ交絡（`Confounder.Name=AI_ROLE_JEV`）として検出される。
- MCP は読み取りツール`ai_roles`（API §3.35）で、役割ごとの設定・最後の起動で使い始めた組・履歴を読める。秘密は返さず、キーの有無も返さない。

## テスト（モックだけ）

| 内容 | テスト |
|---|---|
| 既定のままで Jev・Sol・Opus・Luna・トライアルが前と同じ URL・キー・モデルで呼ばれる | `airolewire.TestDefaultsCallTheSameEndpointKeyAndModelAsBefore` |
| OpenRouter を選んだ役割・アームが OpenRouter のキーとモデル id で呼ばれる（独自 API には送らない） | `airolewire.TestOpenRouterRolesCallOpenRouterWithItsKeyAndModel`・`trialflow.TestAppWiring_ArmProviderPicksTheConnection`・`runflow.TestBuildServices_LiveJevFollowsItsRoleSetting` |
| ライブの Jev のプロバイダー・モデルは MCP から変えられない | `settingstests.TestAIRoleSettingsAreNotWritableFromTheMCP` |
| MCP の読み取りで秘密が出ない（`secrets`を読まない） | `settingstests.TestAIRolesToolReadsTheSettingsAndNoSecrets`・`TestAIRolesToolDefaultsAndNeverTouchesTheSecretsTable` |
| 画面の並び・保存・履歴 | `airolesroute`の各テスト |
| ライブの Jev の切り替えが実験の交絡になる | `experimenttests.TestLiveJevRoleSwitchConfoundsAnExperiment` |

## 守っていること

Risk・発注・執行・ブローカー・Paper ガード・リスクと資金の既定値・既存のシャドーのコードは変えていない。Setup Guard は従来どおり`JEV_API_KEY`を必須とする（Jev の役割を OpenRouter にしても、`JEV_API_KEY`の入力は求められる。これを緩めるかは別の判断）。
