# Mux Skin 1 · 小 Mux 皮肤创作规范

入口：AI 助手 → 外观 → 小 Mux 衣柜。导入、预览后点击「安装并应用」。
所有图片和设置保存在当前应用的本机 WebView 存储中，不上传服务器。浏览器预览与桌面应用分别保存；清除应用浏览数据会移除皮肤，建议保留导出的皮肤包。

桌面端导出皮肤、示例包和规范时会弹出原生保存对话框，选择位置后保存。浏览器预览使用浏览器下载功能。

## 快速制作

准备透明背景的静态 PNG，在衣柜选择「从图片创建」，填写名称和作者，调整气泡锚点，预览后安装或导出。无需编辑 JSON。最大边 2048 px，最小边 32 px，宽高比 1:4 到 4:1。显示大小可在衣柜调整为 72–200 px（最长边）。

进阶创作者可以下载 `mux-starter.muxskin`，作为 ZIP 解压后替换图片和编辑 `manifest.json`。压缩时应让描述文件直接位于 ZIP 根目录，再将扩展名改为 `.muxskin`。支持普通 ZIP 的 Stored、Deflate 压缩，不支持分卷、加密或 ZIP64。

## 目录与描述文件

```text
my-character.muxskin
├── manifest.json
└── assets/
    ├── idle.png
    └── thinking.png
```

```json
{
  "schemaVersion": 1,
  "id": "com.example.moon-cat",
  "name": "月猫",
  "author": "作者昵称",
  "version": "1.0.0",
  "preview": "assets/idle.png",
  "canvas": { "width": 128, "height": 128 },
  "anchor": { "x": 0.5, "y": 1 },
  "states": {
    "idle": { "type": "image", "src": "assets/idle.png" },
    "thinking": {
      "type": "spritesheet",
      "src": "assets/thinking.png",
      "columns": 2,
      "frames": 2,
      "fps": 2,
      "loop": true
    }
  }
}
```

| 字段 | 约定 |
| --- | --- |
| schemaVersion | 固定为数字 1；不支持的版本会拒绝导入 |
| id | 3–80 位小写字母、数字、点、短横线；以字母或数字开头；保留 builtin.default、preferences |
| name / author | 必填，1–80 字符，无控制字符 |
| version | 必填，1–32 字符；建议使用 1.0.0 格式 |
| preview | 可选，包内静态 PNG 路径；衣柜实时预览使用状态图片 |
| canvas | 每帧画布尺寸；32–2048 px；宽高比 1:4–4:1 |
| anchor | 气泡相对角色画布的连接参考点；x、y 为 0–1；左上角为 (0,0)，底部中央为 (0.5,1) |
| states | idle 必需；其他状态可选；仅支持下表的状态名 |

同 ID 视为同一个皮肤，安装预览会提示替换；点击「安装并应用」后覆盖已有版本。皮肤名称不会改变 AI 的身份、提示词或权限。

## 动作与播放

| 状态 | 触发 |
| --- | --- |
| idle | 待机 |
| thinking | 助手正在处理任务 |
| waiting | 有操作等待确认 |
| replying | 新回复摘要正在展示 |
| hover | 鼠标悬停在助手区域 |
| dragging | 拖动角色 |

优先级：waiting > thinking > dragging > hover > replying > idle。缺失状态回退至 idle；应用仍显示任务进度、确认数量。切换动作时从第 0 帧开始。关闭动画或启用减少动态效果时显示首帧；页面隐藏或角色离开可视区后停止播放。非循环动画播放完成停留在末帧。

`image`：单张静态 PNG，尺寸等于 canvas。

开启角色动画后，应用为 `image` 状态提供轻量动作：待机漂浮、思考摇摆、等待轻浮、回复弹跳、悬停招呼、拖动倾斜。现有皮肤包无需修改。精灵图保留自身逐帧动作，不叠加这套位移动画。关闭角色动画、启用减少动态效果、页面隐藏或角色离开可视区域时停止播放；动作只移动角色图像，不改变拖动热区或气泡定位。

`spritesheet`：将等尺寸帧从左至右、从上至下排列在一张 PNG 中。columns 为列数（1–64，不能大于帧数），frames 为有效帧数（1–120），fps 为整数帧率（1–30），loop 为布尔值。图片宽度必须为 canvas.width × columns，高度必须为 canvas.height × ceil(frames / columns)，末行未使用格子留空。所有状态使用相同画布与角色位置，避免动作切换时跳动。

不支持 GIF、APNG、WebP、SVG、Live2D、音频、脚本或远程资源。V1 使用静态 PNG 与由应用控制播放的精灵图。

## 文件限制与错误处理

- ZIP 最大 20 MiB；解压后总文件大小最多 32 MiB；最多 32 个条目（包含目录）；描述文件最多 16 KiB。
- 单张 PNG 任一边不超过 8192 px；全部 PNG 总像素最多 16,777,216（包含预览图和未引用图片）。
- 路径最多 160 字符，只允许英文字母、数字、下划线、点、短横线和 `/`。必须为相对路径，不可包含空路径段、`.`、`..`、反斜线、网址或重复路径。
- 包内仅允许 manifest.json、静态 .png 和目录。校验 ZIP/PNG 完整性、资源存在性、解码能力与帧尺寸。
- 安装失败保留当前皮肤；读取时遇到损坏包会跳过并显示提示。图片运行时加载失败回退至默认角色。
- 最多安装 20 个自定义皮肤。删除使用中的皮肤后自动恢复默认。可导出后分享或备份。

## English quick reference

Open AI assistant → Appearance. Import a `.muxskin` ZIP, preview, then install. A transparent PNG can also be converted with Create from image. Assets stay in the current app's local WebView storage. Keep exports as backups.

The example manifest above is complete. `idle` is required; optional states fall back to idle. Spritesheets are row-major with frame dimensions equal to `canvas`; columns 1–64, frames 1–120, fps 1–30. `anchor` is a normalized bubble reference point. Only static PNG assets and manifest.json are accepted. Archive limits: 20 MiB compressed, 32 MiB expanded, 32 entries, 16,777,216 total pixels. Same-ID installs replace the existing skin after the explicit install action.
