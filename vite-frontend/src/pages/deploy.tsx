import type { NodeApiItem } from "@/api/types";
import type { DragEndEvent } from "@dnd-kit/core";

import { useCallback, useEffect, useMemo, useState } from "react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import { DndContext, closestCenter } from "@dnd-kit/core";
import { SortableContext, rectSortingStrategy } from "@dnd-kit/sortable";

import { AnimatedPage } from "@/components/animated-page";
import {
  DragHandle,
  SortableCard,
  applyOrder,
  draggedIds,
  orderItems,
  reorderIds,
  useSortSensors,
} from "@/components/sortable";
import { PageLoadingState } from "@/components/page-state";
import { PageIntro } from "@/components/page-intro";
import { LabeledSwitch } from "@/components/labeled-switch";
import { SelectableCard } from "@/components/selectable-card";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import { getNodeList } from "@/api";
import {
  copyDeployment,
  createDeployments,
  deleteDeployment,
  deploySettings,
  getDeployCommand,
  getDeployDefaults,
  getDeployment,
  getDeployments,
  refreshDeployment,
  runDeployments,
  syncDeployNodes,
  uninstallDeployment,
  updateDeployOrder,
  updateDeployment,
  type DeployParams,
  type DeploySettings,
  type Deployment,
} from "@/api/enhanced";
import { copyText, formatDateTime } from "@/utils/enhanced";

const modalClassNames = {
  base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
};

const PROTOCOLS = [
  { key: "ss", label: "Shadowsocks 2022", hint: "流量 1.00×，TCP + UDP" },
  { key: "mieru", label: "mieru", hint: "低熵 48 约 1.34×，TCP + UDP 同端口" },
  {
    key: "sudoku",
    label: "Sudoku",
    hint: "上行约 4×，下行 packed 约 1.33×，仅 TCP",
  },
];

const SS_CIPHERS = [
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-ietf-poly1305",
];

const IP_VERSIONS = ["ipv4-prefer", "ipv6-prefer", "ipv4", "ipv6", "dual"];
const LOW_ENTROPY = [
  { key: "off", label: "off（约 1.00×）" },
  { key: "56", label: "56（约 1.15×）" },
  { key: "48", label: "48（约 1.34×，默认）" },
  { key: "40", label: "40（约 1.61×）" },
  { key: "32", label: "32（约 2.01×）" },
];
const SUDOKU_TABLES = [
  { key: "prefer_entropy", label: "prefer_entropy（推荐）" },
  { key: "prefer_ascii", label: "prefer_ascii" },
  { key: "up_ascii_down_entropy", label: "up_ascii_down_entropy" },
  { key: "up_entropy_down_ascii", label: "up_entropy_down_ascii" },
];

const OP_LABEL: Record<string, string> = {
  install: "部署中",
  info: "刷新中",
  uninstall: "卸载中",
};

const statusChip = (item: Pick<Deployment, "status" | "taskOp">) => {
  switch (item.status) {
    case "success":
      return { color: "success" as const, label: "已部署" };
    case "running":
      return {
        color: "primary" as const,
        label: OP_LABEL[item.taskOp || ""] || "执行中",
      };
    case "failed":
      return { color: "danger" as const, label: "失败" };
    case "uninstalled":
      return { color: "warning" as const, label: "已卸载" };
    default:
      return { color: "default" as const, label: "待部署" };
  }
};

const randomHex = (bytes: number) => {
  const buf = new Uint8Array(bytes);

  crypto.getRandomValues(buf);

  return Array.from(buf, (b) => b.toString(16).padStart(2, "0")).join("");
};

const randomSSPassword = (cipher: string) => {
  const len =
    cipher === "2022-blake3-aes-128-gcm"
      ? 16
      : cipher.startsWith("2022-")
        ? 32
        : 0;

  if (len === 0) return randomHex(16);
  const buf = new Uint8Array(len);

  crypto.getRandomValues(buf);

  return btoa(String.fromCharCode(...buf));
};

const randomUUID = () =>
  typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${randomHex(4)}-${randomHex(2)}-4${randomHex(2).slice(1)}-a${randomHex(2).slice(1)}-${randomHex(6)}`;

// 新建 / 复制后的提示：每条未能开始的部署对应一条 warning
const batchMessage = (
  verb: string,
  count: number,
  run: boolean,
  warnings: string[] = [],
) => {
  if (!run) return `${verb} ${count} 条部署`;
  const started = Math.max(count - warnings.length, 0);

  return started > 0
    ? `${verb} ${count} 条部署，${started} 个节点开始部署`
    : `${verb} ${count} 条部署，但都没有开始部署`;
};

const nodeAddress = (node: NodeApiItem) =>
  String(node.serverIp || node.serverIpV4 || node.serverIpV6 || "");

const isOnline = (node: NodeApiItem) => node.status === 1;

const PROTOCOL_NAMES: Record<string, string> = {
  ss: "SS 2022",
  mieru: "mieru",
  sudoku: "Sudoku",
};

const protocolPort = (params: DeployParams, proto: string) =>
  proto === "ss"
    ? params.ssPort
    : proto === "mieru"
      ? params.mieruPort
      : params.sudokuPort;

// 与原版节点卡片相同的在线状态点
const StatusDot = ({ online }: { online: boolean }) => (
  <span
    className={`h-2.5 w-2.5 flex-shrink-0 rounded-full ${online ? "bg-emerald-500" : "bg-rose-500"}`}
    title={online ? "在线" : "离线"}
  />
);

const InfoRow = ({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) => (
  <div className="flex min-w-0 items-center justify-between gap-2 text-sm">
    <span className="flex-shrink-0 text-default-600">{label}</span>
    <span className="min-w-0 truncate text-right text-xs">{children}</span>
  </div>
);

interface DeployForm {
  id?: number;
  nodeName?: string;
  host: string;
  nodeIds: number[];
  remark: string;
  run: boolean;
  params: DeployParams;
}

export default function DeployPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState<Deployment[]>([]);
  const [flvxNodes, setFlvxNodes] = useState<NodeApiItem[]>([]);
  const [settings, setSettings] = useState<DeploySettings | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsUrl, setSettingsUrl] = useState("");
  const [settingsProxy, setSettingsProxy] = useState(false);

  const [form, setForm] = useState<DeployForm | null>(null);
  const [saving, setSaving] = useState(false);

  const [copyItem, setCopyItem] = useState<Deployment | null>(null);
  const [copyNodeIds, setCopyNodeIds] = useState<number[]>([]);
  const [copyRun, setCopyRun] = useState(true);
  const [commandItem, setCommandItem] = useState<Deployment | null>(null);
  const [commandText, setCommandText] = useState("");
  const [logId, setLogId] = useState<number | null>(null);
  const [logItem, setLogItem] = useState<Deployment | null>(null);
  const [uninstallItem, setUninstallItem] = useState<Deployment | null>(null);
  const [deleteItem, setDeleteItem] = useState<Deployment | null>(null);
  const [deleteNodes, setDeleteNodes] = useState(true);
  const [busyId, setBusyId] = useState<number | null>(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [listRes, nodeRes, settingsRes] = await Promise.all([
        getDeployments(),
        getNodeList(),
        deploySettings({}),
      ]);

      if (listRes.code === 0 && listRes.data) setItems(listRes.data);
      else if (!silent) toast.error(listRes.msg || "加载部署记录失败");
      if (nodeRes.code === 0 && nodeRes.data) setFlvxNodes(nodeRes.data);
      if (settingsRes.code === 0 && settingsRes.data)
        setSettings(settingsRes.data);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // 有任务在执行时定时刷新列表
  const anyRunning = items.some((i) => i.status === "running");

  useEffect(() => {
    if (!anyRunning) return;
    const timer = window.setInterval(() => void load(true), 3000);

    return () => window.clearInterval(timer);
  }, [anyRunning, load]);

  // 日志窗口打开时跟踪日志
  useEffect(() => {
    if (logId === null) return;
    let stopped = false;
    const fetchLog = async () => {
      const res = await getDeployment(logId);

      if (!stopped && res.code === 0 && res.data) setLogItem(res.data);
    };

    void fetchLog();
    const timer = window.setInterval(() => void fetchLog(), 2000);

    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [logId]);

  const nodeById = useMemo(() => {
    const map = new Map<number, NodeApiItem>();

    flvxNodes.forEach((n) => map.set(n.id, n));

    return map;
  }, [flvxNodes]);

  // ─── 拖拽排序 ─────────────────────────────────────────────

  const sortSensors = useSortSensors();

  const handleDragEnd = async (event: DragEndEvent) => {
    const ids = draggedIds(event);

    if (!ids) return;
    const allIds = items.map((d) => d.id);
    const order = reorderIds(allIds, allIds, ids[0], ids[1]);

    if (!order) return;
    const previous = items;

    setItems(applyOrder(items, order));
    const res = await updateDeployOrder(orderItems(order));

    if (res.code !== 0) {
      toast.error(res.msg || "保存排序失败");
      setItems(previous);
    }
  };

  const showWarnings = (warnings?: string[]) =>
    (warnings || []).forEach((w) => toast(w, { icon: "⚠️", duration: 8000 }));

  // ─── 新建 / 编辑 ──────────────────────────────────────────

  const openCreate = async () => {
    const res = await getDeployDefaults();

    if (res.code !== 0 || !res.data) {
      toast.error(res.msg || "生成默认参数失败");

      return;
    }
    setForm({
      host: "",
      nodeIds: [],
      remark: "",
      run: true,
      params: res.data.params,
    });
  };

  const openEdit = (item: Deployment) => {
    setForm({
      id: item.id,
      nodeName: item.nodeName || item.name,
      host: item.host,
      nodeIds: [item.nodeId],
      remark: item.remark || "",
      run: false,
      params: { ...item.params },
    });
  };

  const setParam = <K extends keyof DeployParams>(
    key: K,
    value: DeployParams[K],
  ) => {
    setForm((prev) =>
      prev ? { ...prev, params: { ...prev.params, [key]: value } } : prev,
    );
  };

  const toggleProtocol = (key: string) => {
    setForm((prev) => {
      if (!prev) return prev;
      const set = new Set(prev.params.protocols);

      if (set.has(key)) set.delete(key);
      else set.add(key);

      return {
        ...prev,
        params: {
          ...prev.params,
          protocols: PROTOCOLS.map((p) => p.key).filter((k) => set.has(k)),
        },
      };
    });
  };

  const toggleFormNode = (id: number) => {
    setForm((prev) => {
      if (!prev) return prev;
      const set = new Set(prev.nodeIds);

      if (set.has(id)) set.delete(id);
      else set.add(id);

      return { ...prev, nodeIds: Array.from(set) };
    });
  };

  const regenerate = async () => {
    if (!form) return;
    const res = await getDeployDefaults();

    if (res.code === 0 && res.data) {
      setForm({
        ...form,
        params: {
          ...res.data.params,
          protocols: form.params.protocols,
          ipVersion: form.params.ipVersion,
          blockPrivate: form.params.blockPrivate,
          mihomoVersion: form.params.mihomoVersion,
        },
      });
      toast.success("已重新生成端口和密钥");
    }
  };

  const submitForm = async () => {
    if (!form) return;
    if (form.params.protocols.length === 0) {
      toast.error("至少选择一个协议");

      return;
    }
    const params: DeployParams = {
      ...form.params,
      ssPort: Number(form.params.ssPort) || 0,
      mieruPort: Number(form.params.mieruPort) || 0,
      sudokuPort: Number(form.params.sudokuPort) || 0,
    };

    setSaving(true);
    try {
      if (form.id) {
        const res = await updateDeployment({
          id: form.id,
          host: form.host.trim(),
          params,
          remark: form.remark,
        });

        if (res.code === 0) {
          toast.success("已保存，重新部署后服务器上的配置才会更新");
          setForm(null);
          void load(true);
        } else {
          toast.error(res.msg || "保存失败");
        }

        return;
      }
      if (form.nodeIds.length === 0) {
        toast.error("请选择要部署的 FLVX 节点");

        return;
      }
      const res = await createDeployments({
        nodeIds: form.nodeIds,
        host: form.nodeIds.length === 1 ? form.host.trim() : "",
        params,
        remark: form.remark,
        run: form.run,
      });

      if (res.code === 0 && res.data) {
        toast.success(
          batchMessage(
            "已创建",
            res.data.items.length,
            form.run,
            res.data.warnings,
          ),
        );
        showWarnings(res.data.warnings);
        setForm(null);
        void load(true);
      } else {
        toast.error(res.msg || "创建失败");
      }
    } finally {
      setSaving(false);
    }
  };

  // ─── 执行 ─────────────────────────────────────────────────

  const runOp = async (
    item: Deployment,
    op: "install" | "info" | "uninstall",
  ) => {
    setBusyId(item.id);
    try {
      const res =
        op === "install"
          ? await runDeployments([item.id])
          : op === "info"
            ? await refreshDeployment(item.id)
            : await uninstallDeployment(item.id);

      if (res.code === 0) {
        toast.success(
          op === "install"
            ? "已下发部署任务"
            : op === "info"
              ? "正在读取服务器上的状态"
              : "已下发卸载任务",
        );
        showWarnings(res.data?.warnings);
        setLogItem(null);
        setLogId(item.id);
        void load(true);
      } else {
        toast.error(res.msg || "操作失败", { duration: 8000 });
      }
    } finally {
      setBusyId(null);
    }
  };

  const openCopy = (item: Deployment) => {
    setCopyNodeIds([]);
    setCopyRun(true);
    setCopyItem(item);
  };

  const submitCopy = async () => {
    if (!copyItem) return;
    if (copyNodeIds.length === 0) {
      toast.error("请选择目标节点");

      return;
    }
    setSaving(true);
    try {
      const res = await copyDeployment(copyItem.id, copyNodeIds, copyRun);

      if (res.code === 0 && res.data) {
        toast.success(
          batchMessage(
            "已复制",
            res.data.items.length,
            copyRun,
            res.data.warnings,
          ),
        );
        showWarnings(res.data.warnings);
        setCopyItem(null);
        void load(true);
      } else {
        toast.error(res.msg || "复制失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const openCommand = async (item: Deployment) => {
    const res = await getDeployCommand(item.id);

    if (res.code === 0 && res.data) {
      setCommandText(res.data.command);
      setCommandItem(item);
    } else {
      toast.error(res.msg || "生成命令失败");
    }
  };

  const generateNodes = async (item: Deployment) => {
    const res = await syncDeployNodes(item.id);

    if (res.code === 0 && res.data) {
      toast.success(`已生成 / 更新 ${res.data.nodes} 个代理节点`);
      showWarnings(res.data.warnings);
      void load(true);
    } else {
      toast.error(res.msg || "生成节点失败");
    }
  };

  const confirmUninstall = async () => {
    if (!uninstallItem) return;
    const item = uninstallItem;

    setUninstallItem(null);
    await runOp(item, "uninstall");
  };

  const confirmDelete = async () => {
    if (!deleteItem) return;
    const res = await deleteDeployment(deleteItem.id, deleteNodes);

    if (res.code === 0) {
      toast.success("已删除");
      setDeleteItem(null);
      void load(true);
    } else {
      toast.error(res.msg || "删除失败");
    }
  };

  const saveSettings = async () => {
    const res = await deploySettings({
      scriptUrl: settingsUrl.trim(),
      useProxy: settingsProxy,
    });

    if (res.code === 0 && res.data) {
      setSettings(res.data);
      setSettingsOpen(false);
      toast.success("已保存");
    } else {
      toast.error(res.msg || "保存失败");
    }
  };

  if (loading) {
    return <PageLoadingState message="正在加载部署记录..." />;
  }

  const p = form?.params;
  const singleFormNode =
    form && form.nodeIds.length === 1 ? nodeById.get(form.nodeIds[0]) : null;
  // 每个 FLVX 节点只能有一条部署
  const deployedNodeIds = new Set(
    items.map((d) => d.nodeId).filter((id) => id > 0),
  );

  const renderNodePicker = (
    selectedIds: number[],
    onToggle: (id: number) => void,
    excludeId?: number,
  ) => {
    const list = flvxNodes.filter((n) => n.id !== excludeId);
    const allTaken =
      list.length > 0 && list.every((n) => deployedNodeIds.has(n.id));

    if (list.length === 0) {
      return (
        <div className="rounded-lg border border-dashed border-divider py-6 text-center text-sm text-default-500">
          还没有可用的 FLVX 节点，请先在
          <button
            className="mx-1 text-primary"
            type="button"
            onClick={() => navigate("/node")}
          >
            节点
          </button>
          页面添加并安装节点端。
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {allTaken && (
          <p className="text-xs text-warning">
            这些节点都已经有部署了（每个节点只能部署一次），请先在「节点」页面添加新的
            FLVX 节点。
          </p>
        )}
        <div className="grid max-h-[260px] grid-cols-1 gap-2 overflow-auto pr-1 md:grid-cols-2">
          {list.map((n) => {
            const taken = deployedNodeIds.has(n.id);
            const checked = !taken && selectedIds.includes(n.id);

            return (
              <SelectableCard
                key={n.id}
                className="items-center"
                disabled={taken}
                selected={checked}
                onToggle={() => onToggle(n.id)}
              >
                <Checkbox
                  className="pointer-events-none"
                  isDisabled={taken}
                  isSelected={checked}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-medium">
                      {n.name}
                    </span>
                    <StatusDot online={isOnline(n)} />
                  </div>
                  <div className="truncate font-mono text-xs text-default-500">
                    {nodeAddress(n) || "-"}
                  </div>
                </div>
                {taken && (
                  <Chip
                    className="h-5 flex-shrink-0 px-1 text-[10px]"
                    size="sm"
                    variant="flat"
                  >
                    已部署
                  </Chip>
                )}
              </SelectableCard>
            );
          })}
        </div>
      </div>
    );
  };

  return (
    <AnimatedPage className="px-3 lg:px-6 py-8 space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
        <PageIntro
          className=""
          description={
            <>
              选择一台或多台 FLVX 节点，由面板通过节点端执行{" "}
              <a
                className="text-primary"
                href="https://github.com/mozziexwz/mihomo_script"
                rel="noreferrer"
                target="_blank"
              >
                mihomo_script
              </a>
              ，一键部署 Shadowsocks 2022 / mieru /
              Sudoku，不需要登录服务器。部署完成后自动在「代理节点」生成节点；部署参数可以复制到其他节点。仅管理员可用。
            </>
          }
          title="部署节点"
        />
        <div className="flex shrink-0 flex-wrap gap-2">
          <Button
            size="sm"
            variant="flat"
            onPress={() => {
              setSettingsUrl(settings?.scriptUrl || "");
              setSettingsProxy(Boolean(settings?.useProxy));
              setSettingsOpen(true);
            }}
          >
            脚本设置
          </Button>
          <Button
            size="sm"
            variant="flat"
            onPress={() => navigate("/proxy-node")}
          >
            代理节点
          </Button>
          <Button color="primary" size="sm" variant="flat" onPress={openCreate}>
            新建部署
          </Button>
        </div>
      </div>

      {items.length === 0 ? (
        <Card className="shadow-sm border border-gray-200 dark:border-gray-700 bg-default-50/50">
          <CardBody className="text-center py-20 flex flex-col items-center justify-center min-h-[200px]">
            <h3 className="text-xl font-medium text-foreground tracking-tight mb-2">
              暂无部署
            </h3>
            <p className="text-default-500 text-sm max-w-md mx-auto leading-relaxed">
              点击「新建部署」，选择 FLVX 节点和协议。节点需要是带 systemd 的
              Debian / Ubuntu / RHEL 系服务器，节点端需升级到 myflvx 2.2.2
              或更高版本。
            </p>
          </CardBody>
        </Card>
      ) : (
        <DndContext
          collisionDetection={closestCenter}
          sensors={sortSensors}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={items.map((d) => d.id)}
            strategy={rectSortingStrategy}
          >
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4">
              {items.map((item) => {
                const chip = statusChip(item);
                const running = item.status === "running";
                const busy = busyId === item.id;
                const noNode = item.nodeId <= 0;

                return (
                  <SortableCard key={item.id} id={item.id}>
                    {(handle) => (
                      <Card className="group relative h-full flex flex-col overflow-visible border border-divider shadow-sm transition-shadow duration-200 hover:shadow-md">
                        <CardHeader className="pb-3 md:pb-3">
                          <div className="flex w-full items-start justify-between gap-2">
                            <div className="flex min-w-0 flex-1 items-start gap-1">
                              <DragHandle
                                className="-ml-2 flex-shrink-0 opacity-100 focus-visible:opacity-100 sm:opacity-0 sm:group-hover:opacity-100"
                                handle={handle}
                              />
                              <div className="min-w-0 flex-1 pt-0.5">
                                <div className="flex min-w-0 items-center gap-2">
                                  <h3 className="truncate text-sm font-semibold leading-5 text-foreground">
                                    {item.name}
                                  </h3>
                                  {!noNode && (
                                    <StatusDot online={item.nodeOnline} />
                                  )}
                                </div>
                              </div>
                            </div>
                            <Chip
                              className="h-5 flex-shrink-0 px-1 text-[10px]"
                              color={chip.color}
                              size="sm"
                              variant="flat"
                            >
                              {chip.label}
                            </Chip>
                          </div>
                        </CardHeader>
                        <CardBody className="flex flex-1 flex-col pt-0 pb-3 md:pt-0 md:pb-3">
                          <div className="mb-4 space-y-2">
                            <InfoRow label="地址">
                              <span className="font-mono" title={item.host}>
                                {item.host || "-"}
                              </span>
                            </InfoRow>
                            {item.protocols.map((proto) => (
                              <InfoRow
                                key={proto}
                                label={PROTOCOL_NAMES[proto] || proto}
                              >
                                <span className="font-mono">
                                  {protocolPort(item.params, proto) || "-"}
                                </span>
                              </InfoRow>
                            ))}
                            <InfoRow label="代理节点">
                              {item.proxyNodes} 个
                            </InfoRow>
                            <InfoRow label="上次执行">
                              {item.lastRunTime
                                ? formatDateTime(item.lastRunTime)
                                : "-"}
                            </InfoRow>
                            {item.remark && (
                              <InfoRow label="备注">
                                <span title={item.remark}>{item.remark}</span>
                              </InfoRow>
                            )}
                            {noNode && (
                              <div className="text-xs text-warning">
                                没有关联 FLVX 节点，不能由面板执行
                              </div>
                            )}
                          </div>
                          <div className="mt-auto space-y-1.5">
                            <div className="grid grid-cols-3 gap-1.5">
                              <Button
                                className="min-h-8"
                                color="success"
                                isDisabled={running || noNode}
                                isLoading={busy}
                                size="sm"
                                variant="flat"
                                onPress={() => runOp(item, "install")}
                              >
                                {item.status === "pending"
                                  ? "部署"
                                  : "重新部署"}
                              </Button>
                              <Button
                                className="min-h-8"
                                color="warning"
                                isDisabled={running || busy || noNode}
                                size="sm"
                                title="读取服务器上的部署状态并更新代理节点"
                                variant="flat"
                                onPress={() => runOp(item, "info")}
                              >
                                刷新
                              </Button>
                              <Button
                                className="min-h-8"
                                color="secondary"
                                size="sm"
                                variant="flat"
                                onPress={() => {
                                  setLogItem(item);
                                  setLogId(item.id);
                                }}
                              >
                                日志
                              </Button>
                            </div>
                            <div className="grid grid-cols-3 gap-1.5">
                              <Button
                                className="min-h-8"
                                color="primary"
                                isDisabled={running}
                                size="sm"
                                title="编辑部署参数"
                                variant="flat"
                                onPress={() => openEdit(item)}
                              >
                                编辑
                              </Button>
                              <Button
                                className="min-h-8"
                                color="primary"
                                isDisabled={running}
                                size="sm"
                                title="把部署参数复制到其他节点"
                                variant="flat"
                                onPress={() => openCopy(item)}
                              >
                                复制
                              </Button>
                              <Button
                                className="min-h-8"
                                color="warning"
                                isDisabled={
                                  running ||
                                  noNode ||
                                  item.status === "uninstalled"
                                }
                                size="sm"
                                variant="flat"
                                onPress={() => setUninstallItem(item)}
                              >
                                卸载
                              </Button>
                            </div>
                            <div className="grid grid-cols-3 gap-1.5">
                              <Button
                                className="min-h-8"
                                size="sm"
                                title="按部署参数重新生成代理节点"
                                variant="flat"
                                onPress={() => generateNodes(item)}
                              >
                                生成节点
                              </Button>
                              <Button
                                className="min-h-8"
                                size="sm"
                                title="复制手动安装命令（用于未接入面板的服务器）"
                                variant="flat"
                                onPress={() => openCommand(item)}
                              >
                                安装命令
                              </Button>
                              <Button
                                className="min-h-8"
                                color="danger"
                                isDisabled={running}
                                size="sm"
                                variant="flat"
                                onPress={() => {
                                  setDeleteNodes(true);
                                  setDeleteItem(item);
                                }}
                              >
                                删除
                              </Button>
                            </div>
                          </div>
                        </CardBody>
                      </Card>
                    )}
                  </SortableCard>
                );
              })}
            </div>
          </SortableContext>
        </DndContext>
      )}

      {/* 新建 / 编辑 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!form}
        placement="center"
        scrollBehavior="inside"
        size="4xl"
        onOpenChange={(open) => !open && setForm(null)}
      >
        <ModalContent>
          {(onClose) =>
            form && p ? (
              <>
                <ModalHeader className="flex flex-col gap-1">
                  <h2 className="text-xl font-bold">
                    {form.id ? `编辑部署：${form.nodeName || ""}` : "新建部署"}
                  </h2>
                  <p className="text-small text-default-500">
                    {form.id
                      ? "修改参数后点「重新部署」，服务器上的配置才会更新。"
                      : "选中的节点使用同一组端口和密钥，各自使用自己的地址；保存后由面板通过节点端执行部署。"}
                  </p>
                </ModalHeader>
                <ModalBody>
                  <div className="space-y-4">
                    {form.id ? (
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <Input
                          readOnly
                          description="名称与 FLVX 节点保持一致，代理节点名为「节点名-SS」等"
                          label="FLVX 节点"
                          value={form.nodeName || `#${form.nodeIds[0]}`}
                          variant="bordered"
                        />
                        <Input
                          description="客户端连接用的 IP 或域名"
                          label="连接地址"
                          value={form.host}
                          variant="bordered"
                          onChange={(e) =>
                            setForm({ ...form, host: e.target.value })
                          }
                        />
                      </div>
                    ) : (
                      <>
                        <div>
                          <div className="mb-2 flex items-center justify-between">
                            <span className="text-sm font-medium">
                              选择 FLVX 节点（可多选，每个节点只能部署一次）
                            </span>
                            <span className="text-xs text-default-500">
                              已选 {form.nodeIds.length} 个
                            </span>
                          </div>
                          {renderNodePicker(form.nodeIds, toggleFormNode)}
                        </div>
                        <Input
                          description={
                            form.nodeIds.length > 1
                              ? "选择多个节点时各自使用节点地址；部署名称与节点名称相同"
                              : "客户端连接用的 IP 或域名，留空使用节点地址；部署名称与节点名称相同"
                          }
                          isDisabled={form.nodeIds.length > 1}
                          label="连接地址（可选）"
                          placeholder={
                            singleFormNode ? nodeAddress(singleFormNode) : ""
                          }
                          value={form.nodeIds.length > 1 ? "" : form.host}
                          variant="bordered"
                          onChange={(e) =>
                            setForm({ ...form, host: e.target.value })
                          }
                        />
                      </>
                    )}

                    <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                      {PROTOCOLS.map((proto) => (
                        <SelectableCard
                          key={proto.key}
                          className="items-start"
                          selected={p.protocols.includes(proto.key)}
                          onToggle={() => toggleProtocol(proto.key)}
                        >
                          <Checkbox
                            className="pointer-events-none"
                            isSelected={p.protocols.includes(proto.key)}
                          />
                          <div>
                            <div className="text-sm font-medium">
                              {proto.label}
                            </div>
                            <div className="text-xs text-default-500">
                              {proto.hint}
                            </div>
                          </div>
                        </SelectableCard>
                      ))}
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                      <Select
                        label="出站 IP"
                        selectedKeys={[p.ipVersion]}
                        onSelectionChange={(keys) =>
                          setParam(
                            "ipVersion",
                            String(Array.from(keys)[0] ?? "ipv4-prefer"),
                          )
                        }
                      >
                        {IP_VERSIONS.map((v) => (
                          <SelectItem key={v}>{v}</SelectItem>
                        ))}
                      </Select>
                      <Input
                        description="留空安装最新正式版"
                        label="mihomo 版本"
                        placeholder="例如 v1.19.33"
                        value={p.mihomoVersion || ""}
                        variant="bordered"
                        onChange={(e) =>
                          setParam("mihomoVersion", e.target.value.trim())
                        }
                      />
                      <LabeledSwitch
                        description="开启后不能经此节点访问服务器自身（含它的公网 IP）和内网地址"
                        isSelected={p.blockPrivate}
                        label="禁止经代理访问本机和内网"
                        onValueChange={(v) => setParam("blockPrivate", v)}
                      />
                    </div>

                    {p.protocols.includes("ss") && (
                      <Card className="border border-divider shadow-none">
                        <CardHeader className="py-2">
                          <span className="font-medium text-sm">
                            Shadowsocks 2022
                          </span>
                        </CardHeader>
                        <CardBody className="pt-0 grid grid-cols-1 md:grid-cols-3 gap-4">
                          <Input
                            label="端口"
                            type="number"
                            value={String(p.ssPort || "")}
                            variant="bordered"
                            onChange={(e) =>
                              setParam("ssPort", Number(e.target.value))
                            }
                          />
                          <Select
                            label="加密方式"
                            selectedKeys={[
                              p.ssCipher || "2022-blake3-aes-128-gcm",
                            ]}
                            onSelectionChange={(keys) => {
                              const cipher = String(
                                Array.from(keys)[0] ??
                                  "2022-blake3-aes-128-gcm",
                              );

                              setForm((prev) =>
                                prev
                                  ? {
                                      ...prev,
                                      params: {
                                        ...prev.params,
                                        ssCipher: cipher,
                                        ssPassword: randomSSPassword(cipher),
                                      },
                                    }
                                  : prev,
                              );
                            }}
                          >
                            {SS_CIPHERS.map((c) => (
                              <SelectItem key={c}>{c}</SelectItem>
                            ))}
                          </Select>
                          <Input
                            description="2022 加密须为对应长度密钥的 base64"
                            endContent={
                              <button
                                className="text-xs text-primary"
                                type="button"
                                onClick={() =>
                                  setParam(
                                    "ssPassword",
                                    randomSSPassword(
                                      p.ssCipher || "2022-blake3-aes-128-gcm",
                                    ),
                                  )
                                }
                              >
                                随机
                              </button>
                            }
                            label="密码"
                            value={p.ssPassword || ""}
                            variant="bordered"
                            onChange={(e) =>
                              setParam("ssPassword", e.target.value.trim())
                            }
                          />
                        </CardBody>
                      </Card>
                    )}

                    {p.protocols.includes("mieru") && (
                      <Card className="border border-divider shadow-none">
                        <CardHeader className="py-2">
                          <span className="font-medium text-sm">
                            mieru（TCP + UDP 同端口）
                          </span>
                        </CardHeader>
                        <CardBody className="pt-0 grid grid-cols-1 md:grid-cols-4 gap-4">
                          <Input
                            label="端口"
                            type="number"
                            value={String(p.mieruPort || "")}
                            variant="bordered"
                            onChange={(e) =>
                              setParam("mieruPort", Number(e.target.value))
                            }
                          />
                          <Input
                            label="用户名"
                            value={p.mieruUser || ""}
                            variant="bordered"
                            onChange={(e) =>
                              setParam("mieruUser", e.target.value.trim())
                            }
                          />
                          <Input
                            description="8-64 位字母数字 _.-"
                            endContent={
                              <button
                                className="text-xs text-primary"
                                type="button"
                                onClick={() =>
                                  setParam("mieruPassword", randomHex(16))
                                }
                              >
                                随机
                              </button>
                            }
                            label="密码"
                            value={p.mieruPassword || ""}
                            variant="bordered"
                            onChange={(e) =>
                              setParam("mieruPassword", e.target.value.trim())
                            }
                          />
                          <Select
                            label="低熵档位"
                            selectedKeys={[p.mieruLowEntropy || "48"]}
                            onSelectionChange={(keys) =>
                              setParam(
                                "mieruLowEntropy",
                                String(Array.from(keys)[0] ?? "48"),
                              )
                            }
                          >
                            {LOW_ENTROPY.map((v) => (
                              <SelectItem key={v.key}>{v.label}</SelectItem>
                            ))}
                          </Select>
                        </CardBody>
                      </Card>
                    )}

                    {p.protocols.includes("sudoku") && (
                      <Card className="border border-divider shadow-none">
                        <CardHeader className="py-2">
                          <span className="font-medium text-sm">
                            Sudoku（HTTP 伪装固定关闭）
                          </span>
                        </CardHeader>
                        <CardBody className="pt-0 space-y-4">
                          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                            <Input
                              label="端口"
                              type="number"
                              value={String(p.sudokuPort || "")}
                              variant="bordered"
                              onChange={(e) =>
                                setParam("sudokuPort", Number(e.target.value))
                              }
                            />
                            <Select
                              description={
                                p.sudokuKeyMode === "pair"
                                  ? "每台服务器生成自己的密钥对，部署完成后面板自动读取客户端密钥"
                                  : "面板生成共享密钥，所有选中的节点使用同一个密钥"
                              }
                              label="密钥"
                              selectedKeys={[p.sudokuKeyMode || "psk"]}
                              onSelectionChange={(keys) => {
                                const mode = String(
                                  Array.from(keys)[0] ?? "psk",
                                );

                                setForm((prev) =>
                                  prev
                                    ? {
                                        ...prev,
                                        params: {
                                          ...prev.params,
                                          sudokuKeyMode: mode,
                                          sudokuKey:
                                            mode === "psk"
                                              ? prev.params.sudokuKey ||
                                                randomUUID()
                                              : "",
                                          sudokuServerKey: "",
                                        },
                                      }
                                    : prev,
                                );
                              }}
                            >
                              <SelectItem key="psk">
                                共享密钥（面板生成）
                              </SelectItem>
                              <SelectItem key="pair">
                                ED25519 密钥对（服务器生成）
                              </SelectItem>
                            </Select>
                            {p.sudokuKeyMode !== "pair" && (
                              <Input
                                description="16-255 位字母数字或 -"
                                endContent={
                                  <button
                                    className="text-xs text-primary"
                                    type="button"
                                    onClick={() =>
                                      setParam("sudokuKey", randomUUID())
                                    }
                                  >
                                    随机
                                  </button>
                                }
                                label="共享密钥"
                                value={p.sudokuKey || ""}
                                variant="bordered"
                                onChange={(e) =>
                                  setParam("sudokuKey", e.target.value.trim())
                                }
                              />
                            )}
                          </div>
                          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                            <Select
                              label="编码表"
                              selectedKeys={[p.sudokuTable || "prefer_entropy"]}
                              onSelectionChange={(keys) =>
                                setParam(
                                  "sudokuTable",
                                  String(
                                    Array.from(keys)[0] ?? "prefer_entropy",
                                  ),
                                )
                              }
                            >
                              {SUDOKU_TABLES.map((t) => (
                                <SelectItem key={t.key}>{t.label}</SelectItem>
                              ))}
                            </Select>
                            <Input
                              description="最小-最大（0-100）"
                              label="填充率"
                              value={p.sudokuPadding || "1-15"}
                              variant="bordered"
                              onChange={(e) =>
                                setParam("sudokuPadding", e.target.value.trim())
                              }
                            />
                            <Input
                              description="可选，如 xpxvvpvv,vxpvxvvp"
                              label="自定义编码表"
                              value={p.sudokuCustomTables || ""}
                              variant="bordered"
                              onChange={(e) =>
                                setParam(
                                  "sudokuCustomTables",
                                  e.target.value.trim(),
                                )
                              }
                            />
                          </div>
                          <LabeledSwitch
                            description="开启后下行约 4×；关闭为 packed 下行约 1.33×（默认）"
                            isSelected={p.sudokuPureDownlink}
                            label="纯 Sudoku 下行"
                            onValueChange={(v) =>
                              setParam("sudokuPureDownlink", v)
                            }
                          />
                        </CardBody>
                      </Card>
                    )}

                    <Input
                      label="备注"
                      value={form.remark}
                      variant="bordered"
                      onChange={(e) =>
                        setForm({ ...form, remark: e.target.value })
                      }
                    />
                    {!form.id && (
                      <LabeledSwitch
                        description="关闭后只保存部署记录，稍后在列表中点「部署」"
                        isSelected={form.run}
                        label="保存后立即部署"
                        onValueChange={(v) => setForm({ ...form, run: v })}
                      />
                    )}
                  </div>
                </ModalBody>
                <ModalFooter>
                  <Button variant="light" onPress={onClose}>
                    取消
                  </Button>
                  <Button variant="flat" onPress={regenerate}>
                    重新生成端口和密钥
                  </Button>
                  <Button
                    color="primary"
                    isLoading={saving}
                    onPress={submitForm}
                  >
                    {form.id ? "保存" : form.run ? "保存并部署" : "保存"}
                  </Button>
                </ModalFooter>
              </>
            ) : null
          }
        </ModalContent>
      </Modal>

      {/* 复制到其他节点 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!copyItem}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setCopyItem(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">
                  复制到其他节点：{copyItem?.name}
                </h2>
                <p className="text-small text-default-500">
                  使用相同的协议、端口和密钥在选中的节点上部署，各自使用节点自己的地址，代理节点以节点名命名。
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  {renderNodePicker(
                    copyNodeIds,
                    (id) =>
                      setCopyNodeIds((prev) =>
                        prev.includes(id)
                          ? prev.filter((x) => x !== id)
                          : [...prev, id],
                      ),
                    copyItem?.nodeId,
                  )}
                  <LabeledSwitch
                    isSelected={copyRun}
                    label="复制后立即部署"
                    onValueChange={setCopyRun}
                  />
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" isLoading={saving} onPress={submitCopy}>
                  复制
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 手动安装命令 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!commandItem}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setCommandItem(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">
                  安装命令：{commandItem?.name}
                </h2>
                <p className="text-small text-default-500">
                  用于没有接入面板的服务器：以 root
                  执行这条命令即可部署同样的参数。面板不会跟踪手动执行的部署，也不会自动生成代理节点。
                </p>
              </ModalHeader>
              <ModalBody>
                <pre className="text-xs font-mono whitespace-pre-wrap break-all rounded-lg bg-default-100 p-3">
                  {commandText}
                </pre>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  关闭
                </Button>
                <Button
                  color="primary"
                  onPress={() => copyText(commandText, "安装命令")}
                >
                  复制命令
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 日志 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={logId !== null}
        placement="center"
        scrollBehavior="inside"
        size="4xl"
        onOpenChange={(open) => {
          if (!open) {
            setLogId(null);
            setLogItem(null);
          }
        }}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex items-center gap-2">
                <h2 className="text-xl font-bold">
                  执行日志：{logItem?.name || ""}
                </h2>
                {logItem && (
                  <Chip
                    color={statusChip(logItem).color}
                    size="sm"
                    variant="flat"
                  >
                    {statusChip(logItem).label}
                  </Chip>
                )}
              </ModalHeader>
              <ModalBody>
                <pre className="text-xs font-mono whitespace-pre-wrap break-all rounded-lg bg-default-100 p-3 min-h-[200px] max-h-[60vh] overflow-auto">
                  {logItem?.lastLog || "暂无日志"}
                </pre>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  关闭
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 卸载 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!uninstallItem}
        placement="center"
        size="md"
        onOpenChange={(open) => !open && setUninstallItem(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <h2 className="text-lg font-bold text-warning">卸载 mihomo</h2>
              </ModalHeader>
              <ModalBody>
                <p className="text-default-600">
                  将在 FLVX 节点「{uninstallItem?.nodeName}」上执行
                  mihomo_script uninstall，删除 mihomo
                  和它的全部配置；对应的代理节点会被停用。部署记录会保留，之后可以重新部署。
                </p>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="warning" onPress={confirmUninstall}>
                  确认卸载
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 删除 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!deleteItem}
        placement="center"
        size="md"
        onOpenChange={(open) => !open && setDeleteItem(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <h2 className="text-lg font-bold text-danger">删除部署记录</h2>
              </ModalHeader>
              <ModalBody>
                <p className="text-default-600">
                  只删除面板中的记录，不会卸载服务器上的
                  mihomo；需要卸载请先点「卸载」。
                </p>
                <Checkbox
                  isSelected={deleteNodes}
                  onValueChange={setDeleteNodes}
                >
                  同时删除生成的代理节点（以及它们的中转节点）
                </Checkbox>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="danger" onPress={confirmDelete}>
                  确认删除
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 脚本设置 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={settingsOpen}
        placement="center"
        size="2xl"
        onOpenChange={setSettingsOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">脚本设置</h2>
                <p className="text-small text-default-500">
                  节点端从这个地址下载 mihomo_script 并以 root 执行（只支持
                  https 地址）。
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <Input
                    description={`留空使用默认地址：${settings?.defaultScriptUrl || ""}`}
                    label="mihomo_script 地址"
                    value={settingsUrl}
                    variant="bordered"
                    onChange={(e) => setSettingsUrl(e.target.value)}
                  />
                  <LabeledSwitch
                    description="国内服务器访问 GitHub 困难时可开启（使用「设置」中的 GitHub 加速地址）。请只使用可信的加速地址；脚本下载 mihomo 内核仍需访问 GitHub。"
                    isSelected={settingsProxy}
                    label="通过 GitHub 加速代理下载脚本"
                    onValueChange={setSettingsProxy}
                  />
                  {settings?.effectiveUrl && (
                    <p className="text-xs text-default-500 break-all">
                      当前实际地址：{settings.effectiveUrl}
                    </p>
                  )}
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" onPress={saveSettings}>
                  保存
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
