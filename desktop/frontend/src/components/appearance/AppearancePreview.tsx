import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Input,
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Switch,
  Tab,
  TabList,
  Tooltip,
} from "@fluentui/react-components";
import { Info20Regular, Settings20Regular } from "@fluentui/react-icons";
import { GlassSurface } from "../material/GlassSurface";
import { NetworkHealthBadge } from "../home/NetworkHealthBadge";

export function AppearancePreview({ onToast }: { onToast: () => void }) {
  return (
    <div className="appearance-preview">
      <GlassSurface className="preview-primary">
        <div className="preview-title">
          <div>
            <span className="section-kicker">主玻璃表面</span>
            <h2>聚合引擎</h2>
          </div>
          <Badge appearance="outline"><i className="state-dot" />运行中</Badge>
        </div>
        <strong className="preview-speed">32.1 <small>MB/s</small></strong>
        <TabList defaultSelectedValue="proxy" size="small">
          <Tab value="proxy">系统代理</Tab>
          <Tab value="tun">虚拟网卡 TUN</Tab>
        </TabList>
        <div className="preview-actions">
          <Button appearance="primary" onClick={onToast}>显示 Toast</Button>
          <Tooltip content="这是 Fluent UI Tooltip" relationship="label">
            <Button appearance="subtle" icon={<Info20Regular />} aria-label="Tooltip 示例" />
          </Tooltip>
          <Popover>
            <PopoverTrigger disableButtonEnhancement>
              <Button appearance="secondary" icon={<Settings20Regular />}>Popover</Button>
            </PopoverTrigger>
            <PopoverSurface>次级设置不会创建新的整页层级。</PopoverSurface>
          </Popover>
        </div>
      </GlassSurface>

      <GlassSurface tone="secondary" className="preview-secondary">
        <div className="preview-control">
          <Input defaultValue="192.168.1.108" aria-label="示例输入" />
          <Switch defaultChecked label="启用链路" />
          <Button disabled>禁用状态</Button>
        </div>
        <div className="preview-adapter">
          <span className="preview-adapter-icon">E</span>
          <div>
            <strong>以太网</strong>
            <span>Realtek PCIe 2.5GbE · 192.168.1.108</span>
          </div>
          <strong>20.2 MB/s</strong>
          <NetworkHealthBadge health="healthy" />
        </div>
        <div className="preview-statuses">
          <Badge appearance="tint" color="success">成功</Badge>
          <Badge appearance="tint" color="warning">警告</Badge>
          <Badge appearance="tint" color="danger">错误</Badge>
          <Dialog>
            <DialogTrigger disableButtonEnhancement>
              <Button appearance="subtle">打开 Dialog</Button>
            </DialogTrigger>
            <DialogSurface>
              <DialogBody>
                <DialogTitle>材质预览</DialogTitle>
                <DialogContent>Dialog 继续使用当前强调色和明暗主题。</DialogContent>
                <DialogActions>
                  <DialogTrigger disableButtonEnhancement>
                    <Button appearance="primary">完成</Button>
                  </DialogTrigger>
                </DialogActions>
              </DialogBody>
            </DialogSurface>
          </Dialog>
        </div>
      </GlassSurface>
    </div>
  );
}
