import type { NodeApiItem } from "@/api/types";

import { useCallback, useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";

import {
  AnimatedPage,
  StaggerItem,
  StaggerList,
} from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { LabeledSwitch } from "@/components/labeled-switch";
import { SelectableCard } from "@/components/selectable-card";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
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
  applyDeployOutput,
  createDeployment,
  deleteDeployment,
  deploySettings,
  getDeployDefaults,
  getDeployment,
  getDeployments,
  runDeployment,
  syncDeployNodes,
  updateDeployment,
  type DeployParams,
  type DeploySettings,
  type Deployment,
  type SSHTarget,
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
    hint: "上行约 4×，下行 packed 约 1.4×，仅 TCP",
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

const statusChip = (status: string) => {
  switch (status) {
    case "success":
      return { color: "success" as const, label: "已部署" };
    case "running":
      return { color: "primary" as const, label: "部署中" };
    case "failed":
      return { color: "danger" as const, label: "失败" };
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

interface DeployForm {
  id?: number;
  name: string;
  nodeId: string;
  remark: string;
  params: DeployParams;
}

const emptySSH: SSHTarget = {
  host: "",
  port: 22,
  username: "root",
  authType: "password",
  password: "",
  privateKey: "",
  passphrase: "",
  useSudo: false,
};

export default function DeployPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState<Deployment[]>([]);
  const [flvxNodes, setFlvxNodes] = useState<NodeApiItem[]>([]);
  const [settings, setSettings] = useState<DeploySettings | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsUrl, setSettingsUrl] = useState("");
  const [settingsProxy, setSettingsProxy] = useState(false);

  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<DeployForm | null>(null);
  const [saving, setSaving] = useState(false);

  const [commandItem, setCommandItem] = useState<Deployment | null>(null);
  const [sshItem, setSshItem] = useState<Deployment | null>(null);
  const [ssh, setSsh] = useState<SSHTarget>(emptySSH);
  const [outputItem, setOutputItem] = useState<Deployment | null>(null);
  const [outputText, setOutputText] = useState("");
  const [logItem, setLogItem] = useState<Deployment | null>(null);
  const [deleteItem, setDeleteItem] = useState<Deployment | null>(null);
  const [deleteNodes, setDeleteNodes] = useState(true);
  const [busy, setBusy] = useState(false);
  const pollRef = useRef<number | null>(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [listRes, nodeRes, settingsRes] = await Promise.all([
        getDeployments(),
        getNodeList(),
        deploySettings({}),
      ]);

      if (listRes.code === 0 && listRes.data) setItems(listRes.data);
      else toast.error(listRes.msg || "加载部署记录失败");
      if (nodeRes.code === 0 && nodeRes.data) setFlvxNodes(nodeRes.data);
      if (settingsRes.code === 0 && settingsRes.data)
        setSettings(settingsRes.data);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();

    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
  }, [load]);

  // ─── 新建 / 编辑 ──────────────────────────────────────────

  const openCreate = async () => {
    const res = await getDeployDefaults();

    if (res.code !== 0 || !res.data) {
      toast.error(res.msg || "生成默认参数失败");

      return;
    }
    setForm({ name: "", nodeId: "", remark: "", params: res.data.params });
    setFormOpen(true);
  };

  const openEdit = (item: Deployment) => {
    setForm({
      id: item.id,
      name: item.name,
      nodeId: item.nodeId ? String(item.nodeId) : "",
      remark: item.remark || "",
      params: { ...item.params },
    });
    setFormOpen(true);
  };

  const setParam = <K extends keyof DeployParams>(
    key: K,
    value: DeployParams[K],
  ) => {
    setForm((prev) =>
      prev ? { ...prev, params: { ...prev.params, [key]: value } } : prev,
    );
  };

  const toggleProtocol = (key: string, on: boolean) => {
    setForm((prev) => {
      if (!prev) return prev;
      const set = new Set(prev.params.protocols);

      if (on) set.add(key);
      else set.delete(key);

      return {
        ...prev,
        params: {
          ...prev.params,
          protocols: PROTOCOLS.map((p) => p.key).filter((k) => set.has(k)),
        },
      };
    });
  };

  const pickFlvxNode = (nodeId: string) => {
    setForm((prev) => {
      if (!prev) return prev;
      const node = flvxNodes.find((n) => String(n.id) === nodeId);
      const serverIp = node
        ? String((node as Record<string, unknown>).serverIp || "")
        : "";

      return {
        ...prev,
        nodeId,
        name: prev.name || (node?.name ?? ""),
        params: { ...prev.params, host: serverIp || prev.params.host },
      };
    });
  };

  const regenerate = async () => {
    if (!form) return;
    const res = await getDeployDefaults(form.params.host);

    if (res.code === 0 && res.data) {
      setForm({
        ...form,
        params: {
          ...res.data.params,
          protocols: form.params.protocols,
          host: form.params.host,
        },
      });
      toast.success("已重新生成端口和密钥");
    }
  };

  const submitForm = async () => {
    if (!form) return;
    if (!form.name.trim()) {
      toast.error("请填写名称");

      return;
    }
    if (!form.params.host.trim()) {
      toast.error("请填写客户端连接地址");

      return;
    }
    if (form.params.protocols.length === 0) {
      toast.error("至少选择一个协议");

      return;
    }
    setSaving(true);
    try {
      const payload = {
        name: form.name.trim(),
        nodeId: form.nodeId ? Number(form.nodeId) : 0,
        remark: form.remark,
        params: {
          ...form.params,
          ssPort: Number(form.params.ssPort) || 0,
          mieruPort: Number(form.params.mieruPort) || 0,
          sudokuPort: Number(form.params.sudokuPort) || 0,
        },
      };
      const res = form.id
        ? await updateDeployment({ id: form.id, ...payload })
        : await createDeployment(payload);

      if (res.code === 0 && res.data) {
        toast.success(form.id ? "已保存" : "已创建部署");
        setFormOpen(false);
        await load(true);
        if (!form.id) setCommandItem(res.data);
      } else {
        toast.error(res.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  // ─── 部署执行 ─────────────────────────────────────────────

  const startPolling = (id: number) => {
    if (pollRef.current) window.clearInterval(pollRef.current);
    pollRef.current = window.setInterval(async () => {
      const res = await getDeployment(id);

      if (res.code === 0 && res.data) {
        setLogItem(res.data);
        if (res.data.status !== "running") {
          if (pollRef.current) window.clearInterval(pollRef.current);
          pollRef.current = null;
          if (res.data.status === "success")
            toast.success("部署完成，节点已生成");
          else toast.error("部署失败，请查看日志");
          void load(true);
        }
      }
    }, 2000);
  };

  const openSSH = (item: Deployment) => {
    setSsh({ ...emptySSH, host: item.host });
    setSshItem(item);
  };

  const submitSSH = async () => {
    if (!sshItem) return;
    setBusy(true);
    try {
      const res = await runDeployment(sshItem.id, {
        ...ssh,
        port: Number(ssh.port) || 22,
      });

      if (res.code === 0) {
        const id = sshItem.id;

        setSshItem(null);
        const detail = await getDeployment(id);

        if (detail.code === 0 && detail.data) setLogItem(detail.data);
        startPolling(id);
      } else {
        toast.error(res.msg || "启动部署失败");
      }
    } finally {
      setBusy(false);
    }
  };

  const openLog = async (item: Deployment) => {
    const res = await getDeployment(item.id);

    if (res.code === 0 && res.data) {
      setLogItem(res.data);
      if (res.data.status === "running") startPolling(item.id);
    }
  };

  const submitOutput = async () => {
    if (!outputItem) return;
    setBusy(true);
    try {
      const res = await applyDeployOutput(outputItem.id, outputText);

      if (res.code === 0 && res.data) {
        toast.success(
          `已读取 ${res.data.found.join("、")}，生成 ${res.data.nodes} 个节点`,
        );
        (res.data.warnings || []).forEach((w) =>
          toast(w, { icon: "⚠️", duration: 6000 }),
        );
        setOutputItem(null);
        void load(true);
      } else {
        toast.error(res.msg || "解析失败");
      }
    } finally {
      setBusy(false);
    }
  };

  const generateNodes = async (item: Deployment) => {
    const res = await syncDeployNodes(item.id);

    if (res.code === 0 && res.data) {
      toast.success(`已生成 / 更新 ${res.data.nodes} 个节点`);
      (res.data.warnings || []).forEach((w) =>
        toast(w, { icon: "⚠️", duration: 6000 }),
      );
      void load(true);
    } else {
      toast.error(res.msg || "生成节点失败");
    }
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
      void load(true);
    } else {
      toast.error(res.msg || "保存失败");
    }
  };

  if (loading) {
    return <PageLoadingState message="正在加载部署记录..." />;
  }

  const p = form?.params;

  return (
    <AnimatedPage className="px-3 lg:px-6 py-8 space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <p className="text-sm text-default-500 max-w-3xl">
          使用{" "}
          <a
            className="text-primary"
            href="https://github.com/mozziexwz/mihomo_script"
            rel="noreferrer"
            target="_blank"
          >
            mihomo_script
          </a>{" "}
          在服务器上部署 Shadowsocks 2022 / mieru /
          Sudoku。参数由面板生成：复制一键命令到服务器执行，或填写 SSH
          由面板代为执行（凭据只用于本次，不会保存）。部署完成后自动生成到「代理节点」。
        </p>
        <div className="flex gap-2">
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
              点击「新建部署」，选择协议并生成参数。需要服务器为带 systemd 的
              Debian / Ubuntu / RHEL 系，并使用 root（或免密 sudo）。
            </p>
          </CardBody>
        </Card>
      ) : (
        <StaggerList className="grid grid-cols-1 lg:grid-cols-2 2xl:grid-cols-3 gap-4">
          {items.map((item) => {
            const chip = statusChip(item.status);

            return (
              <StaggerItem key={item.id}>
                <Card className="shadow-sm border border-gray-200 dark:border-gray-700 h-full">
                  <CardHeader className="pb-2">
                    <div className="flex justify-between items-start w-full gap-2">
                      <div className="min-w-0">
                        <h3 className="font-semibold truncate">{item.name}</h3>
                        <p className="text-xs text-default-500 font-mono">
                          {item.host}
                          {item.nodeName ? ` · 节点 ${item.nodeName}` : ""}
                        </p>
                      </div>
                      <Chip color={chip.color} size="sm" variant="flat">
                        {chip.label}
                      </Chip>
                    </div>
                  </CardHeader>
                  <CardBody className="pt-0 space-y-3">
                    <div className="flex flex-wrap gap-1">
                      {item.protocols.map((proto) => (
                        <Chip key={proto} size="sm" variant="flat">
                          {proto === "ss"
                            ? `SS ${item.params.ssPort}`
                            : proto === "mieru"
                              ? `mieru ${item.params.mieruPort}`
                              : `Sudoku ${item.params.sudokuPort}`}
                        </Chip>
                      ))}
                    </div>
                    <div className="text-xs text-default-500">
                      已生成节点 {item.proxyNodes} 个 · 上次执行{" "}
                      {formatDateTime(item.lastRunTime)}
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                      <Button
                        size="sm"
                        variant="flat"
                        onPress={() => setCommandItem(item)}
                      >
                        一键命令
                      </Button>
                      <Button
                        color="primary"
                        isDisabled={item.status === "running"}
                        size="sm"
                        variant="flat"
                        onPress={() => openSSH(item)}
                      >
                        SSH 部署
                      </Button>
                      <Button
                        size="sm"
                        variant="flat"
                        onPress={() => openLog(item)}
                      >
                        日志
                      </Button>
                      <Button
                        size="sm"
                        variant="flat"
                        onPress={() => {
                          setOutputText("");
                          setOutputItem(item);
                        }}
                      >
                        粘贴部署输出
                      </Button>
                      <Button
                        size="sm"
                        variant="flat"
                        onPress={() => generateNodes(item)}
                      >
                        生成节点
                      </Button>
                      <Button
                        isDisabled={item.status === "running"}
                        size="sm"
                        variant="flat"
                        onPress={() => openEdit(item)}
                      >
                        编辑
                      </Button>
                      <Button
                        color="danger"
                        isDisabled={item.status === "running"}
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
                  </CardBody>
                </Card>
              </StaggerItem>
            );
          })}
        </StaggerList>
      )}

      {/* 新建 / 编辑 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={formOpen && !!form}
        placement="center"
        scrollBehavior="inside"
        size="4xl"
        onOpenChange={setFormOpen}
      >
        <ModalContent>
          {(onClose) =>
            form && p ? (
              <>
                <ModalHeader className="flex flex-col gap-1">
                  <h2 className="text-xl font-bold">
                    {form.id ? "编辑部署" : "新建部署"}
                  </h2>
                  <p className="text-small text-default-500">
                    修改参数后需要重新执行部署（命令或
                    SSH），服务器上的配置才会更新。
                  </p>
                </ModalHeader>
                <ModalBody>
                  <div className="space-y-4">
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                      <Input
                        isRequired
                        description="同时作为节点名前缀，如 HK-01 → HK-01-SS"
                        label="名称"
                        value={form.name}
                        variant="bordered"
                        onChange={(e) =>
                          setForm({ ...form, name: e.target.value })
                        }
                      />
                      <Select
                        description="可选，自动填入节点地址"
                        label="对应的 FLVX 节点"
                        placeholder="不关联"
                        selectedKeys={form.nodeId ? [form.nodeId] : []}
                        onSelectionChange={(keys) =>
                          pickFlvxNode(String(Array.from(keys)[0] ?? ""))
                        }
                      >
                        {flvxNodes.map((n) => (
                          <SelectItem key={String(n.id)}>{n.name}</SelectItem>
                        ))}
                      </Select>
                      <Input
                        isRequired
                        description="客户端连接用的 IP 或域名"
                        label="连接地址"
                        value={p.host}
                        variant="bordered"
                        onChange={(e) => setParam("host", e.target.value)}
                      />
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                      {PROTOCOLS.map((proto) => (
                        <SelectableCard
                          key={proto.key}
                          className="items-start"
                          selected={p.protocols.includes(proto.key)}
                          onToggle={() =>
                            toggleProtocol(
                              proto.key,
                              !p.protocols.includes(proto.key),
                            )
                          }
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
                        description="拒绝访问 127/8、10/8、192.168/16、云元数据等地址"
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
                                  ? "服务器生成 ED25519 密钥对；需要 SSH 部署或粘贴部署输出读取客户端密钥"
                                  : "面板生成共享密钥，部署后即可直接生成节点"
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
                    保存
                  </Button>
                </ModalFooter>
              </>
            ) : null
          }
        </ModalContent>
      </Modal>

      {/* 一键命令 */}
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
                <h2 className="text-xl font-bold">一键部署命令</h2>
                <p className="text-small text-default-500">
                  以 root 在服务器（{commandItem?.host}
                  ）执行。云服务器还需要在安全组放行对应端口。
                </p>
              </ModalHeader>
              <ModalBody>
                <pre className="text-xs font-mono whitespace-pre-wrap break-all rounded-lg bg-default-100 p-3">
                  {commandItem?.command}
                </pre>
                {commandItem?.params.protocols.includes("sudoku") &&
                  commandItem.params.sudokuKeyMode === "pair" && (
                    <p className="text-sm text-warning-600">
                      Sudoku 使用服务器生成的密钥对：执行完成后，请把输出中的
                      sudoku:// 链接通过「粘贴部署输出」填回面板。
                    </p>
                  )}
                <p className="text-sm text-default-500">
                  执行成功后点击卡片上的「生成节点」，或「粘贴部署输出」让面板核对端口与密钥。
                </p>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  关闭
                </Button>
                <Button
                  color="primary"
                  onPress={() =>
                    commandItem && copyText(commandItem.command, "部署命令")
                  }
                >
                  复制命令
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* SSH 部署 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!sshItem}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setSshItem(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">SSH 部署：{sshItem?.name}</h2>
                <p className="text-small text-default-500">
                  面板通过 SSH
                  执行一键脚本并读取分享链接，凭据只用于本次部署，不会保存。部署通常需要
                  1-3 分钟。
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <Input
                      label="SSH 主机"
                      value={ssh.host}
                      variant="bordered"
                      onChange={(e) =>
                        setSsh({ ...ssh, host: e.target.value.trim() })
                      }
                    />
                    <Input
                      label="端口"
                      type="number"
                      value={String(ssh.port)}
                      variant="bordered"
                      onChange={(e) =>
                        setSsh({ ...ssh, port: Number(e.target.value) || 22 })
                      }
                    />
                    <Input
                      label="用户名"
                      value={ssh.username}
                      variant="bordered"
                      onChange={(e) =>
                        setSsh({ ...ssh, username: e.target.value.trim() })
                      }
                    />
                  </div>
                  <Select
                    label="认证方式"
                    selectedKeys={[ssh.authType]}
                    onSelectionChange={(keys) =>
                      setSsh({
                        ...ssh,
                        authType: String(
                          Array.from(keys)[0] ?? "password",
                        ) as SSHTarget["authType"],
                      })
                    }
                  >
                    <SelectItem key="password">密码</SelectItem>
                    <SelectItem key="private_key">私钥</SelectItem>
                  </Select>
                  {ssh.authType === "password" ? (
                    <Input
                      autoComplete="off"
                      label="密码"
                      type="password"
                      value={ssh.password}
                      variant="bordered"
                      onChange={(e) =>
                        setSsh({ ...ssh, password: e.target.value })
                      }
                    />
                  ) : (
                    <>
                      <Textarea
                        className="font-mono"
                        label="私钥"
                        minRows={5}
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        value={ssh.privateKey}
                        variant="bordered"
                        onChange={(e) =>
                          setSsh({ ...ssh, privateKey: e.target.value })
                        }
                      />
                      <Input
                        autoComplete="off"
                        label="私钥密码（可选）"
                        type="password"
                        value={ssh.passphrase}
                        variant="bordered"
                        onChange={(e) =>
                          setSsh({ ...ssh, passphrase: e.target.value })
                        }
                      />
                    </>
                  )}
                  <Checkbox
                    isSelected={ssh.useSudo}
                    onValueChange={(v) => setSsh({ ...ssh, useSudo: v })}
                  >
                    非 root 用户，使用 sudo（需要免密 sudo）
                  </Checkbox>
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" isLoading={busy} onPress={submitSSH}>
                  开始部署
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
        isOpen={!!logItem}
        placement="center"
        scrollBehavior="inside"
        size="4xl"
        onOpenChange={(open) => {
          if (!open) {
            setLogItem(null);
            if (pollRef.current) window.clearInterval(pollRef.current);
            pollRef.current = null;
          }
        }}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex items-center gap-2">
                <h2 className="text-xl font-bold">部署日志：{logItem?.name}</h2>
                {logItem && (
                  <Chip
                    color={statusChip(logItem.status).color}
                    size="sm"
                    variant="flat"
                  >
                    {statusChip(logItem.status).label}
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

      {/* 粘贴部署输出 */}
      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!outputItem}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={(open) => !open && setOutputItem(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">粘贴部署输出</h2>
                <p className="text-small text-default-500">
                  把脚本输出或 <code>mihomo-sh info</code> 显示的 ss:// /
                  mierus:// / sudoku://
                  链接粘贴到这里，面板会读取端口与密钥并生成节点。
                </p>
              </ModalHeader>
              <ModalBody>
                <Textarea
                  className="font-mono"
                  minRows={8}
                  value={outputText}
                  variant="bordered"
                  onChange={(e) => setOutputText(e.target.value)}
                />
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" isLoading={busy} onPress={submitOutput}>
                  读取并生成节点
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
                  mihomo（如需卸载请在服务器执行{" "}
                  <code>mihomo-sh uninstall</code>）。
                </p>
                <Checkbox
                  isSelected={deleteNodes}
                  onValueChange={setDeleteNodes}
                >
                  同时删除生成的节点（以及它们的中转节点）
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
                    description="国内服务器访问 GitHub 困难时可开启（使用「设置」中的 GitHub 加速地址）。脚本以 root 运行，请只使用可信的加速地址；脚本下载 mihomo 内核仍需访问 GitHub。"
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
