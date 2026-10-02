-- secrets: 更新確認用の UPDATE_GITHUB_TOKEN（issue #265）は、リポジトリの
-- public 化で機能ごと廃止した。許可キーから外れた後は Settings 画面から
-- 削除できず、暗号化済みの認証情報が DB に残り続けるため、保存済みの行を消す。
DELETE FROM secrets WHERE key = 'UPDATE_GITHUB_TOKEN';
