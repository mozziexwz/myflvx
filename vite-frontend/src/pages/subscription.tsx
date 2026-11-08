import { useCallback, useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";

import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { PageIntro } from "@/components/page-intro";
import { LabeledSwitch } from "@/components/labeled-switch";
import { Card, CardBody } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@/shadcn-bridge/heroui/modal";
import {
  Table,
  TableHeader,
  TableColumn,
  TableBody,
  TableRow,
  TableCell,
} from "@/shadcn-bridge/heroui/table";
import {
  createSubscription,
  deleteSubscriptions,
  downloadMySubscription,
  getSubscription,
  getSubscriptions,
  refreshSubscription,
  updateSubscription,
  type Subscription,
  type SubscriptionMutation,
} from "@/api/enhanced";
import {
  formatBytes,
  formatDate,
  formatDateTime,
  readFileText,
  saveTextFile,
} from "@/utils/enhanced";

const modalClassNames = {
  base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
};

const typeLabel = (type: string) => {
  switch (type) {
    case "upload":
      return "上传";
    case "import":
      return "导入";
    case "generate":
      return "生成";
    default:
      return type;
  }
};

const typeColor = (type: string) => {
  switch (type) {
    case "upload":
      return "primary" as const;
    case "import":
      return "warning" as const;
    default:
      return "success" as const;
  }
};

interface FormState {
  id?: number;
  type: "upload" | "import";
  name: string;
  remark: string;
  fileName: string;
  content: string;
  sourceUrl: string;
  userAgent: string;
  autoUpdate: string;
  enabled: boolean;
}

const emptyForm: FormState = {
  type: "upload",
  name: "",
  remark: "",
  fileName: "",
  content: "",
  sourceUrl: "",
  userAgent: "",
  autoUpdate: "0",
  enabled: true,
};

export default function SubscriptionPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState<Subscription[]>([]);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Subscription | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const res = await getSubscriptions();

      if (res.code === 0 && res.data) setItems(res.data);
      else toast.error(res.msg || "加载订阅失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = (type: "upload" | "import") => {
    setForm({ ...emptyForm, type });
    setFormOpen(true);
  };

  const openEdit = async (item: Subscription) => {
    if (item.type === "generate") {
      navigate(`/sub-generator?id=${item.id}`);

      return;
    }
    const res = await getSubscription(item.id);

    if (res.code !== 0 || !res.data) {
      toast.error(res.msg || "加载订阅失败");

      return;
    }
    const s = res.data;

    setForm({
      id: s.id,
      type: s.type === "import" ? "import" : "upload",
      name: s.name,
      remark: s.remark || "",
      fileName: s.fileName?.replace(/\.(yaml|yml|txt)$/i, "") || "",
      content: s.type === "upload" ? s.content || "" : "",
      sourceUrl: s.sourceUrl || "",
      userAgent: s.userAgent || "",
      autoUpdate: String(s.autoUpdate || 0),
      enabled: s.enabled,
    });
    setFormOpen(true);
  };

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];

    if (!file) return;
    try {
      const text = await readFileText(file);

      setForm((p) => ({
        ...p,
        content: text,
        name: p.name || file.name.replace(/\.(yaml|yml|txt)$/i, ""),
      }));
    } catch {
      toast.error("读取文件失败");
    } finally {
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  const submit = async () => {
    if (!form.name.trim()) {
      toast.error("请填写订阅名称");

      return;
    }
    const payload: SubscriptionMutation = {
      id: form.id,
      name: form.name.trim(),
      remark: form.remark,
      type: form.type,
      fileName: form.fileName.trim(),
      enabled: form.enabled,
      autoUpdate: Number(form.autoUpdate) || 0,
    };

    if (form.type === "upload") {
      if (!form.id && !form.content.trim()) {
        toast.error("请上传或粘贴配置文件内容");

        return;
      }
      payload.content = form.content;
    } else {
      if (!form.sourceUrl.trim()) {
        toast.error("请填写订阅地址");

        return;
      }
      payload.sourceUrl = form.sourceUrl.trim();
      payload.userAgent = form.userAgent.trim();
    }
    setSaving(true);
    try {
      const res = form.id
        ? await updateSubscription(payload)
        : await createSubscription(payload);

      if (res.code === 0) {
        toast.success(form.id ? "已保存" : "已创建");
        setFormOpen(false);
        void load(true);
      } else {
        toast.error(res.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const download = async (item: Subscription) => {
    setBusyId(item.id);
    try {
      const res = await downloadMySubscription(item.id, "clash");

      if (res.code === 0 && res.data) {
        saveTextFile(res.data.content, res.data.fileName, res.data.mime);
      } else {
        toast.error(res.msg || "下载失败");
      }
    } finally {
      setBusyId(null);
    }
  };

  const refresh = async (item: Subscription) => {
    setBusyId(item.id);
    try {
      const res = await refreshSubscription(item.id);

      if (res.code === 0) {
        toast.success(item.type === "import" ? "已重新拉取" : "已重新生成");
        void load(true);
      } else {
        toast.error(res.msg || "刷新失败");
      }
    } finally {
      setBusyId(null);
    }
  };

  const toggleEnabled = async (item: Subscription, enabled: boolean) => {
    const base: SubscriptionMutation = {
      id: item.id,
      name: item.name,
      remark: item.remark,
      type: item.type,
      fileName: item.fileName?.replace(/\.(yaml|yml|txt)$/i, ""),
      enabled,
      autoUpdate: item.autoUpdate,
    };
    const res = await updateSubscription(base);

    if (res.code === 0) {
      setItems((prev) =>
        prev.map((s) => (s.id === item.id ? { ...s, enabled } : s)),
      );
    } else {
      toast.error(res.msg || "操作失败");
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    const res = await deleteSubscriptions([deleteTarget.id]);

    if (res.code === 0) {
      toast.success("已删除");
      setDeleteTarget(null);
      void load(true);
    } else {
      toast.error(res.msg || "删除失败");
    }
  };

  if (loading) {
    return <PageLoadingState message="正在加载订阅..." />;
  }

  return (
    <AnimatedPage className="px-3 lg:px-6 py-8">
      <PageIntro
        description="管理你自己的订阅：上传 Clash / mihomo 配置文件、从机场订阅地址导入，或在「生成订阅」中用自己的代理节点生成。订阅只属于你自己，只能登录面板后下载，不会生成公网订阅链接。"
        title="订阅管理"
      />
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-end mb-4 gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="flat" onPress={() => openCreate("upload")}>
            上传配置
          </Button>
          <Button size="sm" variant="flat" onPress={() => openCreate("import")}>
            从订阅地址导入
          </Button>
          <Button
            color="primary"
            size="sm"
            variant="flat"
            onPress={() => navigate("/sub-generator")}
          >
            生成订阅
          </Button>
        </div>
      </div>

      {items.length === 0 ? (
        <Card className="shadow-sm border border-gray-200 dark:border-gray-700 bg-default-50/50">
          <CardBody className="text-center py-20 flex flex-col items-center justify-center min-h-[240px]">
            <h3 className="text-xl font-medium text-foreground tracking-tight mb-2">
              暂无订阅
            </h3>
            <p className="text-default-500 text-sm max-w-md mx-auto leading-relaxed">
              上传 Clash / mihomo
              配置文件、从其他订阅地址导入，或用「代理节点」里的节点生成订阅。
            </p>
          </CardBody>
        </Card>
      ) : (
        <Card>
          <Table
            aria-label="订阅列表"
            className="overflow-x-auto min-w-full"
            classNames={{
              th: "bg-default-100/50 text-default-600 font-semibold text-sm border-b border-divider py-3 whitespace-nowrap",
              td: "py-3 border-b border-divider/50 group-data-[last=true]:border-b-0",
              tr: "hover:bg-default-50/50 transition-colors",
            }}
          >
            <TableHeader>
              <TableColumn>订阅</TableColumn>
              <TableColumn>类型</TableColumn>
              <TableColumn>节点</TableColumn>
              <TableColumn>流量 / 到期</TableColumn>
              <TableColumn>更新时间</TableColumn>
              <TableColumn>启用</TableColumn>
              <TableColumn>操作</TableColumn>
            </TableHeader>
            <TableBody items={items}>
              {(item: Subscription) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <div className="min-w-[140px]">
                      <div className="font-medium text-sm">{item.name}</div>
                      <div className="text-xs text-default-400">
                        {item.fileName}
                        {item.remark ? ` · ${item.remark}` : ""}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Chip color={typeColor(item.type)} size="sm" variant="flat">
                      {typeLabel(item.type)}
                    </Chip>
                  </TableCell>
                  <TableCell>
                    <span className="text-sm">
                      {item.nodeCount < 0 ? "全部启用节点" : item.nodeCount}
                    </span>
                  </TableCell>
                  <TableCell>
                    <div className="text-xs text-default-500 whitespace-nowrap">
                      {item.subInfo && item.subInfo.total > 0 && (
                        <div>
                          {formatBytes(
                            item.subInfo.upload + item.subInfo.download,
                          )}{" "}
                          / {formatBytes(item.subInfo.total)}
                        </div>
                      )}
                      {item.subInfo?.expire ? (
                        <div>
                          机场到期 {formatDate(item.subInfo.expire * 1000)}
                        </div>
                      ) : null}
                      {!item.subInfo && "-"}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-default-500 whitespace-nowrap">
                      {formatDateTime(item.lastSyncTime || item.updatedTime)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Switch
                      isSelected={item.enabled}
                      size="sm"
                      onValueChange={(v) => toggleEnabled(item, v)}
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-1.5 min-w-max">
                      <Button
                        className="h-6 px-2 min-w-0 text-xs"
                        isLoading={busyId === item.id}
                        size="sm"
                        variant="flat"
                        onPress={() => download(item)}
                      >
                        下载
                      </Button>
                      {item.type !== "upload" && (
                        <Button
                          className="h-6 px-2 min-w-0 text-xs"
                          isDisabled={busyId === item.id}
                          size="sm"
                          variant="flat"
                          onPress={() => refresh(item)}
                        >
                          {item.type === "import" ? "拉取" : "重新生成"}
                        </Button>
                      )}
                      <Button
                        className="h-6 px-2 min-w-0 text-xs bg-indigo-50 text-indigo-600 hover:bg-indigo-100 dark:bg-indigo-950/30 dark:text-indigo-400"
                        size="sm"
                        variant="flat"
                        onPress={() => openEdit(item)}
                      >
                        编辑
                      </Button>
                      <Button
                        className="h-6 px-2 min-w-0 text-xs bg-rose-50 text-rose-600 hover:bg-rose-100 dark:bg-rose-950/30 dark:text-rose-400"
                        size="sm"
                        variant="flat"
                        onPress={() => setDeleteTarget(item)}
                      >
                        删除
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </Card>
      )}

      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={formOpen}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={setFormOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">
                  {form.id
                    ? "编辑订阅"
                    : form.type === "upload"
                      ? "上传配置文件"
                      : "从订阅地址导入"}
                </h2>
                <p className="text-small text-default-500">
                  {form.type === "upload"
                    ? "上传 Clash / mihomo 配置文件（YAML）。粘贴分享链接或 base64 订阅时会按「常用分流」模板生成完整配置。"
                    : "面板定时或手动从订阅地址拉取配置（拒绝内网地址），只能在面板内下载。"}
                </p>
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                      isRequired
                      label="订阅名称"
                      value={form.name}
                      variant="bordered"
                      onChange={(e) =>
                        setForm((p) => ({ ...p, name: e.target.value }))
                      }
                    />
                    <Input
                      description="下载时的文件名，默认使用订阅名称"
                      label="文件名（可选）"
                      value={form.fileName}
                      variant="bordered"
                      onChange={(e) =>
                        setForm((p) => ({ ...p, fileName: e.target.value }))
                      }
                    />
                  </div>
                  {form.type === "upload" ? (
                    <>
                      <div className="flex items-center gap-2">
                        <input
                          ref={fileInputRef}
                          accept=".yaml,.yml,.txt,.conf"
                          className="hidden"
                          type="file"
                          onChange={handleFile}
                        />
                        <Button
                          size="sm"
                          variant="flat"
                          onPress={() => fileInputRef.current?.click()}
                        >
                          选择文件
                        </Button>
                        <span className="text-xs text-default-500">
                          {form.content
                            ? `${form.content.length} 字符`
                            : "未选择文件"}
                        </span>
                      </div>
                      <Textarea
                        className="font-mono"
                        label={form.id ? "配置内容（留空不修改）" : "配置内容"}
                        minRows={8}
                        value={form.content}
                        variant="bordered"
                        onChange={(e) =>
                          setForm((p) => ({ ...p, content: e.target.value }))
                        }
                      />
                    </>
                  ) : (
                    <>
                      <Input
                        isRequired
                        label="订阅地址"
                        placeholder="https://..."
                        value={form.sourceUrl}
                        variant="bordered"
                        onChange={(e) =>
                          setForm((p) => ({ ...p, sourceUrl: e.target.value }))
                        }
                      />
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <Input
                          label="User-Agent（可选）"
                          placeholder="默认 clash.meta"
                          value={form.userAgent}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((p) => ({
                              ...p,
                              userAgent: e.target.value,
                            }))
                          }
                        />
                        <Input
                          description="用户下载时超过该小时数会先重新拉取，0 表示只手动更新"
                          label="自动更新（小时）"
                          type="number"
                          value={form.autoUpdate}
                          variant="bordered"
                          onChange={(e) =>
                            setForm((p) => ({
                              ...p,
                              autoUpdate: e.target.value,
                            }))
                          }
                        />
                      </div>
                    </>
                  )}
                  <Input
                    label="备注"
                    value={form.remark}
                    variant="bordered"
                    onChange={(e) =>
                      setForm((p) => ({ ...p, remark: e.target.value }))
                    }
                  />
                  <LabeledSwitch
                    description="停用后不在「订阅下载」页显示"
                    isSelected={form.enabled}
                    label="启用"
                    onValueChange={(v) =>
                      setForm((p) => ({ ...p, enabled: v }))
                    }
                  />
                </div>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button color="primary" isLoading={saving} onPress={submit}>
                  保存
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={modalClassNames}
        isOpen={!!deleteTarget}
        placement="center"
        size="md"
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>
                <h2 className="text-lg font-bold text-danger">确认删除</h2>
              </ModalHeader>
              <ModalBody>
                <p className="text-default-600">
                  确定删除订阅「{deleteTarget?.name}」吗？删除后无法恢复。
                </p>
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
    </AnimatedPage>
  );
}
