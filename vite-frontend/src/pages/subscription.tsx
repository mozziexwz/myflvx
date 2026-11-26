import type { DragEndEvent } from "@dnd-kit/core";

import { useCallback, useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import { DndContext, closestCenter } from "@dnd-kit/core";
import {
  SortableContext,
  rectSortingStrategy,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import {
  ChevronDown,
  CircleHelp,
  Ellipsis,
  LayoutGrid,
  List,
} from "lucide-react";

import { AnimatedPage } from "@/components/animated-page";
import {
  DragHandle,
  SortableCard,
  SortableTableRow,
  applyOrder,
  draggedIds,
  orderItems,
  reorderIds,
  useSortSensors,
} from "@/components/sortable";
import { PageLoadingState } from "@/components/page-state";
import { PageIntro } from "@/components/page-intro";
import { LabeledSwitch } from "@/components/labeled-switch";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Dropdown,
  DropdownMenu,
  DropdownMenuSeparator,
  DropdownTrigger,
} from "@/shadcn-bridge/heroui/dropdown";
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
  updateSubscriptionOrder,
  type Subscription,
  type SubscriptionMutation,
} from "@/api/enhanced";
import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import {
  copyText,
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

type DownloadFormat = "clash" | "base64" | "uri";
type SubscriptionAction = DownloadFormat | "refresh";

function SubscriptionBadges({ item }: { item: Subscription }) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      <Chip
        className="bg-default-100 text-default-600"
        size="sm"
        variant="flat"
      >
        {typeLabel(item.type)}
      </Chip>
      {item.genConfig?.ipv4Only && (
        <Chip
          className="bg-default-100 text-default-600"
          size="sm"
          variant="flat"
        >
          仅 IPv4
        </Chip>
      )}
    </div>
  );
}

function SubscriptionUsage({ info }: { info: Subscription["subInfo"] }) {
  if (!info || (!info.total && !info.expire)) return null;

  return (
    <>
      {info.total > 0 && (
        <div>
          流量 {formatBytes(info.upload + info.download)} /{" "}
          {formatBytes(info.total)}
        </div>
      )}
      {info.expire > 0 && <div>到期 {formatDate(info.expire * 1000)}</div>}
    </>
  );
}

const nodeCountLabel = (item: Subscription) =>
  item.nodeCount < 0 ? "全部启用节点" : `${item.nodeCount} 个节点`;

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
  const [pendingActions, setPendingActions] = useState(
    () => new Map<number, SubscriptionAction>(),
  );
  const [deleteTarget, setDeleteTarget] = useState<Subscription | null>(null);
  const [viewMode, setViewMode] = useLocalStorageState<"card" | "list">(
    "myflvx_sub_view_mode",
    "card",
  );
  const [guideOpen, setGuideOpen] = useState(false);
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

  const runItemAction = async (
    id: number,
    action: SubscriptionAction,
    perform: () => Promise<void>,
  ) => {
    if (pendingActions.has(id)) return;
    setPendingActions((current) => new Map(current).set(id, action));
    try {
      await perform();
    } finally {
      setPendingActions((current) => {
        const next = new Map(current);

        next.delete(id);

        return next;
      });
    }
  };

  const download = (item: Subscription, format: DownloadFormat) =>
    runItemAction(item.id, format, async () => {
      const res = await downloadMySubscription(item.id, format);

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "获取订阅失败");

        return;
      }
      const { content, fileName, mime } = res.data;

      if (format === "uri") {
        await copyText(content.trim(), "分享链接");
      } else {
        saveTextFile(content, fileName, mime);
        toast.success(`已下载 ${fileName}`);
      }
    });

  const refresh = (item: Subscription) =>
    runItemAction(item.id, "refresh", async () => {
      const res = await refreshSubscription(item.id);

      if (res.code === 0) {
        toast.success(item.type === "import" ? "已重新拉取" : "已重新生成");
        void load(true);
      } else {
        toast.error(res.msg || "刷新失败");
      }
    });

  const sortSensors = useSortSensors();
  const sorting = useRef(false);

  const handleDragEnd = async (event: DragEndEvent) => {
    const ids = draggedIds(event);

    if (!ids || sorting.current) return;
    const allIds = items.map((s) => s.id);
    const order = reorderIds(allIds, allIds, ids[0], ids[1]);

    if (!order) return;
    sorting.current = true;
    setItems(applyOrder(items, order));
    try {
      const res = await updateSubscriptionOrder(orderItems(order));

      if (res.code !== 0) {
        toast.error(res.msg || "保存排序失败");
        const previousIds = new Set(allIds);

        setItems((current) =>
          applyOrder(current, [
            ...allIds,
            ...current
              .filter((item) => !previousIds.has(item.id))
              .map((item) => item.id),
          ]),
        );
      }
    } finally {
      sorting.current = false;
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

  const renderEnabledSwitch = (item: Subscription) => (
    <Switch
      aria-label={`启用${item.name}`}
      isSelected={item.enabled}
      size="sm"
      onValueChange={(value) => toggleEnabled(item, value)}
    />
  );

  const renderActions = (item: Subscription, alignRight = false) => {
    const pending = pendingActions.get(item.id);
    const downloadDisabled = !item.enabled || Boolean(pending);

    return (
      <div
        className={`flex flex-wrap items-center gap-1.5 ${alignRight ? "justify-end" : ""}`}
      >
        <Button
          className="h-8 px-2 text-xs"
          color="primary"
          isDisabled={downloadDisabled}
          isLoading={pending === "clash"}
          size="sm"
          title={item.enabled ? undefined : "启用订阅后可下载"}
          variant="flat"
          onPress={() => download(item, "clash")}
        >
          下载 YAML
        </Button>
        <Button
          className="h-8 px-2 text-xs"
          size="sm"
          variant="flat"
          onPress={() => openEdit(item)}
        >
          编辑
        </Button>
        <Dropdown placement="bottom-end">
          <DropdownTrigger>
            <Button
              isIconOnly
              aria-label={`${item.name}的更多操作`}
              className="size-8"
              variant="flat"
            >
              <Ellipsis size={16} />
            </Button>
          </DropdownTrigger>
          <DropdownMenu>
            <DropdownMenuItem
              disabled={downloadDisabled}
              onSelect={() => download(item, "base64")}
            >
              下载 Base64
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={downloadDisabled}
              onSelect={() => download(item, "uri")}
            >
              复制分享链接
            </DropdownMenuItem>
            {item.type !== "upload" && (
              <DropdownMenuItem
                disabled={Boolean(pending)}
                onSelect={() => refresh(item)}
              >
                {item.type === "import" ? "拉取更新" : "重新生成"}
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-danger focus:text-danger"
              onSelect={() => setDeleteTarget(item)}
            >
              删除
            </DropdownMenuItem>
          </DropdownMenu>
        </Dropdown>
      </div>
    );
  };

  if (loading) {
    return <PageLoadingState message="正在加载订阅..." />;
  }

  return (
    <AnimatedPage className="space-y-4 px-3 py-8 lg:px-6">
      <div className="flex flex-col justify-between gap-3 xl:flex-row xl:items-start">
        <PageIntro
          className=""
          description="生成、导入或下载自己的订阅。下载时需要登录面板。"
          title="订阅管理"
        />
        <div className="flex flex-wrap items-center gap-2">
          <div
            aria-label="订阅视图"
            className="flex gap-0.5 rounded-lg border border-divider p-0.5"
            role="group"
          >
            <Button
              isIconOnly
              aria-label="卡片视图"
              aria-pressed={viewMode !== "list"}
              className="size-8"
              title="卡片视图"
              variant={viewMode !== "list" ? "flat" : "light"}
              onPress={() => setViewMode("card")}
            >
              <LayoutGrid size={16} />
            </Button>
            <Button
              isIconOnly
              aria-label="列表视图"
              aria-pressed={viewMode === "list"}
              className="size-8"
              title="列表视图"
              variant={viewMode === "list" ? "flat" : "light"}
              onPress={() => setViewMode("list")}
            >
              <List size={16} />
            </Button>
          </div>
          <Button
            aria-controls="subscription-import-guide"
            aria-expanded={guideOpen}
            size="sm"
            startContent={<CircleHelp aria-hidden="true" size={14} />}
            variant="bordered"
            onPress={() => setGuideOpen((open) => !open)}
          >
            导入说明
          </Button>
          <Dropdown placement="bottom-end">
            <DropdownTrigger>
              <Button
                endContent={<ChevronDown aria-hidden="true" size={14} />}
                size="sm"
                variant="flat"
              >
                导入 / 上传
              </Button>
            </DropdownTrigger>
            <DropdownMenu>
              <DropdownMenuItem onSelect={() => openCreate("upload")}>
                上传配置
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => openCreate("import")}>
                从订阅地址导入
              </DropdownMenuItem>
            </DropdownMenu>
          </Dropdown>
          <Button
            color="primary"
            size="sm"
            onPress={() => navigate("/sub-generator")}
          >
            生成订阅
          </Button>
        </div>
      </div>

      <Card hidden={!guideOpen} id="subscription-import-guide">
        <CardBody className="space-y-2 py-3 text-sm md:py-3">
          <h2 className="font-medium">在客户端导入订阅</h2>
          <ul className="list-disc space-y-1 pl-5 text-xs leading-relaxed text-default-600">
            <li>
              Clash Verge Rev、Mihomo Party 和 FlClash
              可在订阅或配置页面导入本地 YAML。
            </li>
            <li>OpenClash 可在配置文件管理中上传本地 YAML。</li>
            <li>
              v2rayN 和 Shadowrocket 可使用 Base64
              文件，或复制分享链接后从剪贴板导入。
            </li>
            <li>
              导入含 SS 2022、mieru 或 Sudoku 节点的 YAML
              时，请使用支持相应协议的 Mihomo 内核版本。
            </li>
          </ul>
        </CardBody>
      </Card>

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
        <DndContext
          collisionDetection={closestCenter}
          sensors={sortSensors}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={items.map((item) => item.id)}
            strategy={
              viewMode === "list"
                ? verticalListSortingStrategy
                : rectSortingStrategy
            }
          >
            {viewMode === "list" ? (
              <Card className="border border-divider shadow-sm">
                <Table
                  aria-label="订阅列表"
                  className="table-fixed"
                  classNames={{
                    th: "border-b border-divider bg-default-100/50 px-1.5 py-2.5 text-xs font-semibold sm:px-2",
                    td: "px-1.5 py-2.5 sm:px-2",
                    tr: "border-divider/60 hover:bg-default-50/60 transition-colors",
                  }}
                >
                  <TableHeader>
                    <TableColumn className="w-8 px-1 sm:w-10">
                      <span className="sr-only">排序</span>
                    </TableColumn>
                    <TableColumn>订阅</TableColumn>
                    <TableColumn className="hidden w-20 lg:table-cell">
                      类型
                    </TableColumn>
                    <TableColumn className="hidden w-24 lg:table-cell">
                      节点
                    </TableColumn>
                    <TableColumn className="hidden w-[15%] xl:table-cell">
                      流量 / 到期
                    </TableColumn>
                    <TableColumn className="hidden w-[16%] xl:table-cell">
                      更新时间
                    </TableColumn>
                    <TableColumn className="w-12 px-1 sm:w-14">
                      启用
                    </TableColumn>
                    <TableColumn className="w-32 px-1 text-right sm:w-44">
                      操作
                    </TableColumn>
                  </TableHeader>
                  <TableBody items={items}>
                    {(item: Subscription) => (
                      <SortableTableRow id={item.id}>
                        {(handle) => (
                          <>
                            <TableCell className="px-1">
                              <DragHandle handle={handle} />
                            </TableCell>
                            <TableCell>
                              <div
                                className="truncate font-medium"
                                title={item.name}
                              >
                                {item.name}
                              </div>
                              <div
                                className="truncate text-xs text-default-500"
                                title={item.fileName}
                              >
                                {item.fileName}
                              </div>
                              {item.remark && (
                                <div
                                  className="truncate text-xs text-default-500"
                                  title={item.remark}
                                >
                                  {item.remark}
                                </div>
                              )}
                              <div className="mt-1 space-y-1 text-xs text-default-600 lg:hidden">
                                <SubscriptionBadges item={item} />
                                <div>{nodeCountLabel(item)}</div>
                              </div>
                              <div className="mt-1 break-words text-xs text-default-500 xl:hidden">
                                <SubscriptionUsage info={item.subInfo} />
                                <time>
                                  {formatDateTime(
                                    item.lastSyncTime || item.updatedTime,
                                  )}
                                </time>
                              </div>
                            </TableCell>
                            <TableCell className="hidden lg:table-cell">
                              <SubscriptionBadges item={item} />
                            </TableCell>
                            <TableCell className="hidden text-xs lg:table-cell">
                              {nodeCountLabel(item)}
                            </TableCell>
                            <TableCell className="hidden break-words text-xs text-default-500 xl:table-cell">
                              {item.subInfo?.total || item.subInfo?.expire ? (
                                <SubscriptionUsage info={item.subInfo} />
                              ) : (
                                "-"
                              )}
                            </TableCell>
                            <TableCell className="hidden text-xs text-default-500 xl:table-cell">
                              {formatDateTime(
                                item.lastSyncTime || item.updatedTime,
                              )}
                            </TableCell>
                            <TableCell className="px-1">
                              {renderEnabledSwitch(item)}
                            </TableCell>
                            <TableCell className="px-1">
                              {renderActions(item, true)}
                            </TableCell>
                          </>
                        )}
                      </SortableTableRow>
                    )}
                  </TableBody>
                </Table>
              </Card>
            ) : (
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
                {items.map((item) => (
                  <SortableCard key={item.id} className="min-w-0" id={item.id}>
                    {(handle) => (
                      <Card className="flex h-full flex-col border-divider transition-shadow hover:shadow-md">
                        <CardHeader className="p-3 pb-2 md:p-4 md:pb-2">
                          <div className="flex items-start justify-between gap-2">
                            <div className="flex min-w-0 items-start gap-1">
                              <DragHandle
                                className="-ml-1 shrink-0"
                                handle={handle}
                              />
                              <div className="min-w-0">
                                <h3
                                  className="truncate text-sm font-semibold"
                                  title={item.name}
                                >
                                  {item.name}
                                </h3>
                                <SubscriptionBadges item={item} />
                              </div>
                            </div>
                            <div className="flex shrink-0 items-center gap-1.5 text-xs text-default-500">
                              启用
                              {renderEnabledSwitch(item)}
                            </div>
                          </div>
                        </CardHeader>
                        <CardBody className="flex flex-1 flex-col gap-3 p-3 pt-0 md:p-4 md:pt-0">
                          <div className="space-y-1 text-xs text-default-600">
                            <div className="truncate" title={item.fileName}>
                              文件 {item.fileName || "-"}
                            </div>
                            {item.remark && (
                              <div className="truncate" title={item.remark}>
                                {item.remark}
                              </div>
                            )}
                            <div>{nodeCountLabel(item)}</div>
                            <SubscriptionUsage info={item.subInfo} />
                            <div>
                              更新{" "}
                              {formatDateTime(
                                item.lastSyncTime || item.updatedTime,
                              )}
                            </div>
                          </div>
                          <div className="mt-auto border-t border-divider pt-3">
                            {renderActions(item)}
                          </div>
                        </CardBody>
                      </Card>
                    )}
                  </SortableCard>
                ))}
              </div>
            )}
          </SortableContext>
        </DndContext>
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
