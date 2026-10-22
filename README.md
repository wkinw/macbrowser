# MacBrowser — User Guide

A Windows file browser with macOS Finder–style color labels, built with Go + WebView2.
Browse local drives and SMB shares (NAS) in a fast tile grid, view images in a
built-in lightbox, tag files/folders with Finder colors that stay visible on macOS,
view 0–5 star photo ratings, see EXIF capture times on thumbnails,
and move files between Windows Explorer and your NAS with full clipboard support.

---

## 1. Requirements

- Windows 10 / 11 (64-bit)
- Microsoft Edge WebView2 Runtime (preinstalled on most systems; if the window is
  blank, install the Evergreen runtime from Microsoft)
- Go 1.20+ — only needed if you build from source (`go build -o macbrowser.exe .`)

## 2. Quick start

Double-click `macbrowser.exe` (or run it from a terminal). A window opens and you
are taken to the last visited folder (or a root view on first run).

### Command-line options

| Flag | Default | Description |
| --- | --- | --- |
| `-addr` | `127.0.0.1:0` | HTTP address to listen on (`:0` = random free port) |
| `-token` | random per launch | HTTP access token; set a fixed value for LAN access |
| `-data` | `%AppData%\macbrowser` | Folder for config and thumbnail cache |
| `-browser` | off | Open in the default browser instead of the built-in window |
| `-noopen` | off | Start the server only, do not open any window |
| `-thumbmem` | `192` | Thumbnail memory cache limit (MB) |
| `-webdir` | (embedded) | Serve front-end files from a folder (development) |
| `-debug` | off | Show a console and enable dev tools |

Example: `macbrowser.exe -addr 127.0.0.1:8890 -token my-secret -noopen`

> The server requires an access token. A random token is generated at each launch
> and passed to the built-in window automatically (the token is removed from the
> address bar after login). For LAN access, pass a fixed `-token` and open
> `http://<host>:<port>/?token=<token>` once; a session cookie is then used.
> API clients can send `Authorization: Bearer <token>` instead. With `-noopen`,
> the console prints the access URL including the token. The connection is plain
> HTTP — use it only on trusted networks or through a VPN.

## 3. The window

- **Toolbar** — Back / Forward / Up, address bar (type a path such as `C:\Photos`
  or `smb://nas/Share` and press Enter), quick filter, deep search, upload,
  sort key + direction, tile size slider, hover-preview delay, and the
  “labels” / “hidden files” toggles.
- **Color bar** — click a color chip to show only items with that label;
  click “All” to clear the filter. Chips combine (click several).
- **Rating bar** — click a star-count chip (1★–5★) to show only files with that
  rating; click “All” to clear. Chips combine. The color and rating bars apply
  together (intersection).
- **Grid** — tiles with thumbnails. Folder tiles show a collage of up to four
  images found inside and a folder-shaped tab tinted with the folder’s label color.
- **Status bar** — item counts and total size; right side shows the current path
  (or clipboard state).

## 4. Browsing

- Double-click a folder to enter it; `Alt+←` / `Alt+→` / `Alt+↑` navigate back,
  forward and up.
- The address bar accepts local paths (`C:\...`, UNC `\\server\share`), `smb://`
  paths for configured connections, and drive letters (`D:\`).
- `F5` or `Ctrl+R` refreshes. `Esc` clears the selection.

## 5. Thumbnails, folder previews, hover preview

- **Tile size** — drag the slider (96–320 px). Larger tiles request larger
  thumbnails.
- **Folder previews** — folders with images inside get a 2×2 collage thumbnail
  (up to 4 images, searched two levels deep). Empty or non-image folders show an
  icon.
- **Hover preview** — rest the mouse on any image or folder tile for the
  configured delay (default **0.5 second**) and a large preview pops up, up to
  **80% of the screen**. It disappears when you move away, click, scroll or open
  something. Change the delay in the toolbar (“preview __ seconds”, 0.5–30 s);
  the setting is remembered. Files without a preview (non-images) show nothing.
- **Capture time** — image tiles show the photo's EXIF capture time (date and
  time, e.g. `2026-08-23 14:22`) as a small badge at the bottom-right corner.
  The same time appears on the right side of the hover-preview caption. Photos
  without EXIF time metadata show no badge.

## 6. Color labels (macOS compatible)

Seven labels, matching Finder: 1 Gray, 2 Green, 3 Purple, 4 Blue, 5 Yellow,
6 Red, 7 Orange. Colors can be set:

- Right-click any tile → click a color swatch (click the first, crossed swatch to clear).
- In the lightbox, press `1`–`7` to tag the current image, `0` to clear.
- Multiple selected tiles are labelled together.

**macOS compatibility** — labels are stored in the formats macOS reads:
Finder tags (`com.apple.metadata:_kMDItemUserTags`), the Finder info color flag,
and (for SMB/NAS shares) an AppleDouble sidecar file (`._filename`). Files tagged
here therefore show the correct color when the same share/folder is opened on a
Mac. Folders are supported as well (folders never get a `._` sidecar).

Notes:

- `._name` files are hidden metadata sidecars. MacBrowser always hides them; if
  Windows Explorer shows them, you can turn on “hidden files” to manage them —
  but deleting them removes Mac metadata for that file.
- On local NTFS drives labels are stored as a file attribute stream; other
  Windows apps simply ignore it.
- Showing/hiding the colored dots is controlled by the “labels” checkbox in the
  toolbar.

**Photo ratings (0–5 stars)** — ratings are read-only in MacBrowser. The stars
appear in gold at the bottom-left of the tile (and in the lightbox top bar) and
can be used with the rating bar above the grid to filter by star count. Ratings
are read from `com.apple.metadata:kMDItemStarRating` (binary plist), the
attribute macOS Finder uses, so ratings given on a Mac are visible here. Local
NTFS uses a file attribute stream; SMB/NAS shares use the `._name` AppleDouble
sidecar. Folders do not support ratings.

Ratings already embedded in the photo by other apps — XMP `xmp:Rating` (what
Windows Explorer shows as “Rating”, also written by Adobe apps) or the EXIF
rating tag — are recognized when no macOS rating metadata exists. The image file
is never rewritten.

## 7. Selecting

- Click a tile to select; `Ctrl+click` to add/remove; `Ctrl+A` selects all.
- Drag on empty space for a rubber-band selection (hold `Ctrl` to add to the
  current selection).
- Arrow keys move the selection; `Enter` opens the item; `F2` renames
  (single item); `Delete` deletes (with confirmation).

## 8. Copy, cut, paste — including Windows Explorer

- `Ctrl+C` copies, `Ctrl+X` cuts, `Ctrl+V` pastes into the current folder (or
  right-click → Paste into a folder).
- **Copying out:** every copy/cut also writes the files to the Windows clipboard
  in the standard format, so you can paste them into Explorer, Outlook, etc.
  - Local files: the original paths are used. If you cut a local file and paste
    it in Explorer, it is **moved** (the source disappears).
  - NAS files: Windows cannot paste `smb://` paths, so the files are downloaded
    to a temporary folder first and the clipboard points there. The temp copy is
    cleaned up automatically after 24 hours.
- **Pasting in:** copy files in Explorer (or any app) and press `Ctrl+V` in
  MacBrowser — they are copied from the Windows clipboard into the current folder.
  If the Windows clipboard changed since your last in-app copy, the app pastes
  the new clipboard content automatically.
- Name conflicts are resolved by adding ` (2)`, ` (3)`, … suffixes.

## 9. Drag & drop, upload, download

- Drop files/folders from Explorer onto the grid (or directly onto a folder tile)
  to upload them to that folder.
- Drag tiles out of the window and drop them in Explorer to download them.
- **Upload** button — pick files to upload to the current folder.
- Right-click → “Download to this computer” saves a copy to your Downloads folder
  (useful for NAS files).

## 10. Lightbox (image viewer)

- Open: double-click an image, or right-click → “View in lightbox”.
- `←` / `→` previous/next, `Home` / `End` first/last, `Space` toggles the
  slideshow, `Esc` (or the ✕) closes.
- `1`–`7` set the color label, `0` clears it.
- When the image has a rating, its stars are shown read-only in the top bar.

## 11. Search

- The filter box (top right, `Ctrl+F`) filters the current folder live as you type.
- “Deep search” searches the current folder and all subfolders recursively.

## 12. SMB / NAS connections

1. Open the connection dialog from the left panel (“+” / add connection).
2. Fill in a display name, the server host (IP or host name), port (default 445),
   an optional share name and starting subfolder, plus user name, optional domain
   and password; save.
3. The connection appears in “Places”; click it to browse at `smb://name`.
4. Credentials are stored in the config file (see below) — keep that file private.

## 13. File operations

Right-click menu items (availability depends on the selection):

- Open / Open with default program / Open with another program…
- View in lightbox (images) · Show in File Explorer (local paths)
- Copy · Cut · Paste into folder · Download to this computer
- Copy path · Rename… · Delete
- Color row (7 labels + clear)

Background right-click: Upload files here…, Paste, New folder…, Refresh.

## 14. Settings (remembered automatically)

| Setting | Where | Notes |
| --- | --- | --- |
| Tile size | Toolbar slider | 96–320 px |
| Hover preview delay | Toolbar input | 0.5–30 s, default 0.5 s |
| Show label dots | “labels” checkbox | |
| Show hidden files | “hidden files” checkbox | files starting with `.` or `$`; `._*` always hidden |
| Sort key / direction | Toolbar | name / size / modified / type, asc/desc |

Preferences and connections are saved to `<data dir>\config.json`
(`%AppData%\macbrowser\config.json` by default). SMB passwords are encrypted at
rest with Windows DPAPI (per-user). Only the same Windows account can decrypt
them — if you copy the file to another account or machine, re-enter the
passwords.

## 15. Troubleshooting

- **“Access denied” when pasting to a local folder** — the destination is
  protected (most often a drive root like `C:\` or a system folder). Paste into
  your Documents/Desktop instead, or run MacBrowser as administrator.
- **Blank window** — install the WebView2 Runtime, or try `-browser` (opens in
  your default browser) to verify the server itself works.
- **Labels not visible in macOS Finder** — tags are written when you set them in
  MacBrowser on the same storage the Mac sees. Check the file in a text editor
  of metadata via `xattr -l` on the Mac; `._` sidecars must be present for SMB
  shares that don't support extended attributes.
- **`._` files visible in Explorer** — they are macOS metadata; MacBrowser hides
  them. Only delete them if you don't need Mac metadata.
- **Slow thumbnails on NAS** — thumbnails are generated on first view and cached
  in memory; large folders over slow links take a moment.
- **Several instances** — each runs its own server on its own port; to avoid
  confusion give each a distinct `-addr` and `-data`.

---

# MacBrowser — 使用說明（繁體中文）

以 Go + WebView2 打造的 Windows 檔案瀏覽器，支援 macOS Finder 風格色標。
用磚塊式縮圖格快速瀏覽本機磁碟與 SMB（NAS）共享，內建燈箱看圖，
可為檔案／資料夾標上 Mac 看得見的 Finder 顏色標籤，
檢視相片的 0–5 星評等、在縮圖上顯示 EXIF 拍攝時間，
並透過完整的剪貼簿支援在 Windows 檔案總管與 NAS 之間搬移檔案。

---

## 1. 系統需求

- Windows 10 / 11（64 位元）
- Microsoft Edge WebView2 Runtime（多數系統已內建；若視窗空白請安裝 Microsoft 的 Evergreen Runtime）
- Go 1.20+ — 僅自行編譯時需要（`go build -o macbrowser.exe .`）

## 2. 快速開始

直接雙擊 `macbrowser.exe`（或從終端機執行）。視窗開啟後會回到上次瀏覽的資料夾（第一次執行顯示磁碟清單）。

### 命令列選項

| 參數 | 預設值 | 說明 |
| --- | --- | --- |
| `-addr` | `127.0.0.1:0` | HTTP 監聽位址（`:0` = 隨機可用埠） |
| `-token` | 每次啟動隨機 | HTTP 存取權杖；固定值供區域網路存取 |
| `-data` | `%AppData%\macbrowser` | 設定檔與縮圖快取目錄 |
| `-browser` | 關 | 以系統預設瀏覽器開啟（不用內建視窗） |
| `-noopen` | 關 | 只啟動伺服器，不開啟任何視窗 |
| `-thumbmem` | `192` | 縮圖記憶體快取上限（MB） |
| `-webdir` | （內嵌） | 從資料夾載入前端檔案（開發用） |
| `-debug` | 關 | 顯示主控台並開啟開發者工具 |

範例：`macbrowser.exe -addr 127.0.0.1:8890 -token my-secret -noopen`

> 伺服器需要存取權杖。每次啟動會隨機產生並自動傳給內建視窗（登入後網址列會
> 移除權杖）。若要從區域網路存取，請以 `-token` 指定固定值，並開啟一次
> `http://<主機>:<埠>/?token=<權杖>`，之後使用 session cookie；API 用戶端也可
> 改用 `Authorization: Bearer <權杖>`。使用 `-noopen` 時，主控台會印出含權杖的
> 存取網址。連線為一般 HTTP，請僅在可信任網路使用或透過 VPN。

## 3. 視窗介紹

- **工具列** — 上一頁／下一頁／上層、位址列（可直接輸入 `C:\照片` 或
  `smb://nas/Share` 後按 Enter）、快速篩選、深入搜尋、上傳、排序欄位與方向、
  縮圖大小滑桿、預覽延遲秒數，以及「色標」「隱藏檔」開關。
- **顏色篩選列** — 點顏色晶片只顯示該色標項目；點「全部」清除篩選。可多選疊加。
- **評等篩選列** — 點「1★」–「5★」晶片只顯示該星等的檔案；點「全部」清除。
  可多選疊加；顏色與評等篩選會同時套用（交集）。
- **方格區** — 縮圖磚。資料夾磚會顯示內部最多四張圖片的拼貼，並帶有資料夾頁籤外框，
  頁籤會染上該資料夾的色標顏色。
- **狀態列** — 顯示項目數與總大小；右側顯示目前路徑（或剪貼簿狀態）。

## 4. 瀏覽

- 雙擊資料夾進入；`Alt+←`／`Alt+→`／`Alt+↑` 上一頁、下一頁、上層。
- 位址列接受本機路徑（`C:\...`、UNC `\\server\share`）、已設定連線的 `smb://` 路徑、
  以及磁碟機代號（`D:\`）。
- `F5` 或 `Ctrl+R` 重新整理；`Esc` 清除選取。

## 5. 縮圖、資料夾預覽、滑鼠停留預覽

- **縮圖大小** — 拖動滑桿（96–320 px），磚越大要求的縮圖解析度越高。
- **資料夾預覽** — 內含圖片的資料夾會顯示 2×2 拼貼縮圖（最多 4 張，往下搜尋兩層）。
  空的或沒有圖片的資料夾顯示圖示。
- **停留預覽** — 滑鼠停在圖片或資料夾磚上達設定秒數（預設 **0.5 秒**），
  即彈出最大可達**螢幕 80%** 的大預覽。滑離、點擊、捲動或開啟項目時自動消失。
  秒數可在工具列「預覽 __ 秒」調整（0.5–30 秒），設定會記住。
  非圖片檔沒有預覽，不會彈出。
- **拍攝時間** — 圖片磚右下角會顯示相片的 EXIF 拍攝時間（日期與時間，例如
  `2026-08-23 14:22`）小標籤；停留預覽的說明列右側也會顯示同樣的時間。
  沒有 EXIF 時間的圖片不顯示。

## 6. 顏色標籤（相容 macOS）

七種顏色，對應 Finder：1 灰、2 綠、3 紫、4 藍、5 黃、6 紅、7 橘。設定方式：

- 在磚上按右鍵 → 點色塊（點最左側帶斜線的色塊＝清除）。
- 燈箱中按 `1`–`7` 為目前圖片上色，`0` 清除。
- 多選後一次上色。

**macOS 相容性** — 色標以 macOS 讀得懂的格式寫入：
Finder tags（`com.apple.metadata:_kMDItemUserTags`）、Finder info 色旗標，
以及（SMB／NAS 共享）AppleDouble 側車檔（`._檔名`）。
因此在本程式標色的檔案，在 Mac 上開啟同一共享／資料夾時會顯示正確顏色。
資料夾也支援（資料夾不會產生 `._` 側車檔）。

注意：

- `._檔名` 是隱藏的中介資料側車檔。MacBrowser 一律隱藏它們；若 Windows 檔案總管
  顯示出來，可開啟「隱藏檔」管理——但刪除它們會讓該檔案在 Mac 上失去中介資料。
- 本機 NTFS 磁碟的色標以檔案屬性資料流儲存，其他 Windows 程式會直接忽略。
- 是否顯示磚上的色點由工具列的「色標」核取方塊控制。

**相片評等（0–5 星）** — 星等在此程式中為唯讀。星星以金色顯示在磚的左下角
（燈箱頂端也會顯示），並可用方格上方的「評等篩選列」依星等篩選。評等讀取自
`com.apple.metadata:kMDItemStarRating`（binary plist，macOS Finder 星等使用的
屬性），因此 Mac 上設定的星等在此看得到。本機 NTFS 存於檔案屬性資料流；
SMB／NAS 共享存於 `._檔名` AppleDouble 側車檔。資料夾不支援評等。

若相片本身已內嵌其他程式寫入的評等——XMP `xmp:Rating`（Windows 檔案總管的
「評等」、Adobe 系列也會寫入）或 EXIF 評等標籤——在沒有 macOS 評等中介資料時
也會被辨識。程式不會改寫圖片檔案本身。

## 7. 選取

- 單擊選取；`Ctrl+單擊` 加選／減選；`Ctrl+A` 全選。
- 在空白處拖曳可框選（按住 `Ctrl` 可累加）。
- 方向鍵移動選取；`Enter` 開啟；`F2` 重新命名（單一項目）；`Delete` 刪除（會確認）。

## 8. 複製、剪下、貼上 — 含與檔案總管互通

- `Ctrl+C` 複製、`Ctrl+X` 剪下、`Ctrl+V` 貼到目前資料夾（或在資料夾上按右鍵 → 貼上到此資料夾）。
- **複製到程式外：**複製／剪下時會同時以標準格式寫入 Windows 剪貼簿，可直接貼到
  檔案總管、Outlook 等程式。
  - 本機檔案：使用原始路徑。本機「剪下」後在總管貼上＝**搬移**（來源會消失）。
  - NAS 檔案：Windows 無法貼上 `smb://` 路徑，因此會先下載到暫存資料夾，
    剪貼簿指向暫存檔；暫存檔 24 小時後自動清理。
- **從程式外貼上：**在總管（或任何程式）複製檔案後，在 MacBrowser 按 `Ctrl+V`，
  會從 Windows 剪貼簿複製進目前資料夾。若剪貼簿在你上次程式內複製後被其他程式
  更新，會自動改用新的剪貼簿內容。
- 檔名衝突會自動加上 ` (2)`、` (3)`…。

## 9. 拖放、上傳、下載

- 從檔案總管拖檔案／資料夾到方格區（或直接丟到某個資料夾磚）＝上傳到該資料夾。
- 從視窗把磚拖出去放到檔案總管＝下載。
- **上傳**按鈕 — 選擇要上傳到目前資料夾的檔案。
- 右鍵 → 「下載到此電腦」會在「下載」資料夾存一份（適合 NAS 檔案）。

## 10. 燈箱（看圖）

- 開啟：雙擊圖片，或右鍵 → 「用燈箱檢視」。
- `←`／`→` 上一張／下一張，`Home`／`End` 第一張／最後一張，
  `空白鍵` 開關自動輪播，`Esc`（或 ✕）關閉。
- `1`–`7` 設定色標、`0` 清除。
- 圖片有星等時，頂端會顯示唯讀星星。

## 11. 搜尋

- 篩選框（右上，`Ctrl+F`）即時過濾目前資料夾。
- 「深入搜尋」遞迴搜尋目前資料夾與所有子資料夾。

## 12. SMB／NAS 連線

1. 從左側面板開啟連線對話框（「+」／新增連線）。
2. 填寫顯示名稱、主機（IP 或名稱）、連接埠（預設 445）、可選的共享名稱與起始
   子資料夾，以及使用者名稱、網域（可選）與密碼後儲存。
3. 連線會出現在「位置」清單，點擊即以 `smb://名稱` 瀏覽。
4. 認證資料存在設定檔（見下）——請妥善保管該檔案。

## 13. 檔案操作

右鍵選單項目（依選取內容而異）：

- 開啟／以預設程式開啟／以其他應用程式開啟…
- 用燈箱檢視（圖片）· 在檔案總管中顯示（本機路徑）
- 複製 · 剪下 · 貼上到此資料夾 · 下載到此電腦
- 複製路徑 · 重新命名… · 刪除
- 顏色列（7 色＋清除）

空白處右鍵：上傳檔案到此資料夾…、貼上、新增資料夾…、重新整理。

## 14. 設定（自動記住）

| 設定 | 位置 | 說明 |
| --- | --- | --- |
| 縮圖大小 | 工具列滑桿 | 96–320 px |
| 停留預覽延遲 | 工具列輸入框 | 0.5–30 秒，預設 0.5 秒 |
| 顯示色點 | 「色標」核取方塊 | |
| 顯示隱藏檔 | 「隱藏檔」核取方塊 | 開頭為 `.` 或 `$` 的名稱；`._*` 一律隱藏 |
| 排序欄位／方向 | 工具列 | 名稱／大小／修改日期／類型，升冪／降冪 |

偏好設定與連線存於 `<資料目錄>\config.json`（預設 `%AppData%\macbrowser\config.json`）。
SMB 密碼以 Windows DPAPI（依使用者帳戶）加密儲存；只有同一個 Windows 帳戶能解密。
若將設定檔複製到其他帳戶或電腦，請重新輸入密碼。

## 15. 疑難排解

- **貼到本機資料夾顯示「拒絕存取」** — 目的地受保護（最常見是磁碟根目錄 `C:\`
  或系統資料夾）。請改貼到文件／桌面等一般資料夾，或以系統管理員身分執行。
- **視窗一片空白** — 安裝 WebView2 Runtime；或先用 `-browser`（以預設瀏覽器開啟）
  確認伺服器本身正常。
- **macOS Finder 看不到色標** — 色標是在 MacBrowser 設定時寫入 Mac 看得到的同一儲存位置。
  可在 Mac 上用 `xattr -l` 檢查；不支援擴充屬性的 SMB 共享需有 `._` 側車檔。
- **檔案總管看得到 `._` 檔** — 那是 macOS 中介資料，MacBrowser 會隱藏它們。
  除非不需要 Mac 中介資料，否則不要刪除。
- **NAS 縮圖很慢** — 縮圖在第一次瀏覽時產生並快取於記憶體；慢速連線下的大資料夾
  需要一些時間。
- **同時開多個實例** — 每個實例各自一個伺服器與埠；建議給每個實例不同的
  `-addr` 與 `-data` 以免混淆。
