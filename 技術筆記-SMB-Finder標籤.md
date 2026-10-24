# 技術筆記：SMB 上 Finder 標籤色點修正

## 問題

MacBrowser 設定標籤後，Finder 的「顯示簡介」可看到「綠色」，但檔名旁沒有綠點。主檔的 `com.apple.metadata:_kMDItemUserTags` 已正確包含 `綠色\n2`，但主檔沒有可讀取的 `com.apple.FinderInfo`。

## 根因

程式原先把 32-byte `com.apple.FinderInfo` 當成一般同名 SMB named stream 寫入。macOS SMB 對 FinderInfo 使用特殊的 `AFP_AfpInfo` stream；FinderInfo 是其中 60-byte 結構的一部分，位於 offset 16。只更新 UserTags 或寫 AppleDouble 側車檔，不能取代 Finder 透過 SMB 讀取的 AFPInfo 路徑。

## 修正

- SMB 寫入 `com.apple.FinderInfo` 時，改為讀取並更新 `AFP_AfpInfo` stream 中的 FinderInfo 欄位；不存在時建立有效的 AFPInfo 結構。
- SMB 讀取 FinderInfo 時，從 `AFP_AfpInfo` 的 offset 16 解出 32 bytes。
- 保留 AFPInfo 的其他欄位，並維持 UserTags 與 FinderInfo 色號同步更新；AppleDouble 側車檔仍作為相容性處理。
- 新增 AFPInfo 包裝／解包測試，驗證長度、欄位位置及更新時保留其他欄位。
- Finder 在 Mac 上新增或變更標籤後，app 視窗顯示期間每 5 秒輪詢目前資料夾；輪詢只讀標籤，不執行相片評分分析。

## 驗證

實際重新標記後，Finder 顯示綠點。程式與測試套件完成 Windows x64 交叉編譯；測試套件未在 NAS 上執行整合測試。

## 主要修改檔案

- `internal/vfs/smb.go`：SMB FinderInfo 與 AFP_AfpInfo stream 映射。
- `internal/vfs/smb_test.go`：AFPInfo 結構測試。
- `internal/server/web/app.js`、`internal/server/handlers.go`：標籤差異更新及輕量自動輪詢 API。
- `internal/macmeta/setlabel.go` 與相關測試：同步寫入 UserTags／FinderInfo 的標籤邏輯。
- `README.md`、`internal/macmeta/finderinfo.go`、`internal/server/web/app.js`：標籤名稱及相容性說明／顯示同步。
