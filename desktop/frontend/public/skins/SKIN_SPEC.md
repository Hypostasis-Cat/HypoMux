# Mux Skin 1 / 2 / 3 · 小 Mux 皮肤创作规范

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

V1 不支持 GIF、APNG、WebP、SVG、Live2D、音频、脚本或远程资源，使用静态 PNG 与由应用控制播放的精灵图。

## V2：Live2D 模型包

保留 V1 的基本信息、canvas、anchor、preview 和 states（PNG 加载占位、缩略图和失败回退），将 `schemaVersion` 设为 `2` 并增加：

```json
"live2d": {
  "model": "model/character.model3.json",
  "motions": {
    "thinking": { "group": "Think", "index": 0 },
    "replying": { "group": "Reply", "index": 0 },
    "hover": { "group": "Hello", "index": 0 }
  }
}
```

支持 Cubism 3/4 的 `.moc3`、`.model3.json`、`.motion3.json`、`.physics3.json`、`.pose3.json`、`.exp3.json` 与 PNG 纹理。模型内的路径相对于 model3.json，须遵守下面的路径约束并引用包内文件。每个 JSON 最大 2 MiB；动作组名使用英文字母起始的字母/数字/下划线/短横线，最长 40 字符。动作索引从 0 开始。V2 PNG 总像素限制为 33,554,432，其余 ZIP 大小与条目数限制不变。

只读取模型、纹理、物理、姿势、表情、动作和 EyeBlink/LipSync 参数组；忽略查看器命令、音频、外链、Layout 等额外设置。运行库由应用提供，皮肤包不能携带脚本。模型按画布居中完整显示。自动眨眼需要模型提供 EyeBlink 参数组；呼吸、视线跟随与身体轻摆依赖模型对应参数。状态动作在进入状态时播放，具体动作效果取决于模型，不自动生成飞行或跳跃。

Live2D 最高约 30 fps；关闭动画、减少动态效果、页面隐藏或离开可视区域时暂停。WebGL/模型加载失败会显示 PNG 回退及错误提示。缩略图不启动 WebGL。模型与贴图都在本机读取，不需要模型服务器。运行库与模型使用说明见 [Live2D NOTICE](../live2d/NOTICE.md)。

## 文件限制与错误处理

- ZIP 最大 20 MiB；解压后总文件大小最多 32 MiB；最多 32 个条目（包含目录）；描述文件最多 16 KiB。
- 单张 PNG 任一边不超过 8192 px，单张不超过 16,777,216 像素；V1 全部 PNG 总像素最多 16,777,216，V2 最多 33,554,432（包含预览图和未引用图片）。
- 路径最多 160 字符，只允许英文字母、数字、下划线、点、短横线和 `/`。必须为相对路径，不可包含空路径段、`.`、`..`、反斜线、网址或重复路径。
- V1 包内仅允许 manifest.json、静态 .png 和目录；V2 还允许上述 Live2D 数据文件。校验 ZIP/PNG 完整性、资源存在性、解码能力与帧尺寸。
- 安装失败保留当前皮肤；读取时遇到损坏包会跳过并显示提示。图片运行时加载失败回退至默认角色。
- 最多安装 20 个自定义皮肤。删除使用中的皮肤后自动恢复默认。可导出后分享或备份。

## English quick reference

Open AI assistant → Appearance. Import a `.muxskin` ZIP, preview, then install. A transparent PNG can also be converted with Create from image. Assets stay in the current app's local WebView storage. Keep exports as backups.

The example manifest above is complete. `idle` is required; optional states fall back to idle. Spritesheets are row-major with frame dimensions equal to `canvas`; columns 1–64, frames 1–120, fps 1–30. `anchor` is a normalized bubble reference point. Only static PNG assets and manifest.json are accepted. Archive limits: 20 MiB compressed, 32 MiB expanded, 32 entries, 16,777,216 total pixels. Same-ID installs replace the existing skin after the explicit install action.

## V3：分层动态皮肤

三种类型共用 `.muxskin` ZIP、衣柜、六种状态和独立大小设置：V1 为 PNG / 精灵图，V2 为 Live2D，V3 为分层 PNG。不要同时声明 `live2d` 和 `layered`。

下载 [分层小 Mux 示例](mux-layered.muxskin)，按 ZIP 解压即可编辑。它参照原版小 Mux 的造型重新拆分为身体、眼睛、嘴、额头节点四层，不依赖程序内置 SVG；修改 PNG 和 JSON 即可制作自己的角色。示例的重建脚本位于仓库 `desktop/scripts/create-layered-skin.py`。原版内置角色不受影响。

```json
{
  "schemaVersion": 3,
  "id": "com.example.layered-mux",
  "name": "我的动态精灵",
  "author": "作者",
  "version": "1.0.0",
  "canvas": { "width": 256, "height": 256 },
  "anchor": { "x": 0.5, "y": 0.84 },
  "preview": "preview.png",
  "states": { "idle": { "type": "image", "src": "preview.png" } },
  "layered": {
    "layers": [
      { "id": "body", "src": "body.png", "role": "body" },
      { "id": "eyes", "src": "eyes.png", "role": "eyes", "x": 0.36, "y": 0.50, "width": 0.28, "height": 0.10 },
      { "id": "mouth", "src": "mouth.png", "role": "mouth", "x": 0.44, "y": 0.61, "width": 0.12, "height": 0.06 }
    ]
  }
}
```

- `layers` 按从后到前的绘制顺序排列，1–16 层，恰好一层 `body`。`id` 唯一，以小写字母开头，后续可用小写字母、数字和短横线，最长 32 字符。
- `role`：`body` 身体，`eyes` 自动眨眼，`mouth` 在 `replying` 时自动开合，`decoration` 轻摆。所有部件作为整体呼吸、浮动，思考、悬停、拖动等状态有不同姿态。只有身体也能工作。
- 每个 `src` 是包内透明 PNG；可裁切部件，也可使用完整画布大小。坐标和宽高相对于画布，默认 `x=0,y=0,width=1,height=1`。位置范围 -1–1，宽高 .01–2。图像按目标矩形缩放，建议保持素材比例。
- `pivot` 为相对于部件自身的旋转/缩放中心，默认 `{ "x": 0.5, "y": 0.5 }`，范围 0–1。完整画布的眼睛层需要把 pivot 放到眼睛中心，否则眨眼时会位移。
- `states.idle` 是必填的完整静态合成图，尺寸须等于画布；用于静态缩略图和出错回退。`preview` 也应提供完整角色。
- 仍遵循包体 20 MiB、解压 32 MiB、32 文件、总 PNG 1600 万像素及 manifest 16 KiB 的限制；不允许外部链接、SVG、脚本。

### 自定义部件动作

在某一层加入 `motions`，可使用 `idle / thinking / waiting / replying / hover / dragging` 六种状态：

```json
"motions": {
  "hover": {
    "duration": 800,
    "loop": true,
    "frames": [
      { "rotate": -8, "y": 0 },
      { "rotate": 8, "y": -0.1 },
      { "rotate": -8, "y": 0 }
    ]
  }
}
```

关键帧均匀分布，缓入缓出；每动作 2–16 帧，duration 为 200–30000 毫秒。`loop:false` 播放一次后停在末帧，切换状态后重置。优先使用当前状态轨道，其次自定义 `idle`，最后才使用角色类型的默认动作。定义 `idle` 会覆盖该部件所有未明确指定状态的默认动画。

帧可配置 `x/y`（部件自身宽高的比例，-.5–.5）、`rotate`（角度，-180–180）、`scaleX/scaleY`（.05–3）、`opacity`（0–1）。省略值为位置/旋转 0、缩放/透明度 1。部件轨道与整体呼吸叠加；当前版本是平面图层，不包含骨骼绑定、父子层级或网格变形，复杂变形请使用 Live2D。

关闭角色动画、开启减少动态效果、窗口隐藏或角色离开可视区后会停止动画；重新显示时从头播放。没有 Web Animations 支持的环境显示静止分层形象。

开发者说明：分层示例包及其解压素材不入库。首次本地开发或发布构建前，使用 Python 3 执行 `python desktop/scripts/create-layered-skin.py`（从仓库根目录运行）生成衣柜下载示例。
