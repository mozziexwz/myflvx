import { useCallback, useEffect, useState } from "react";
import toast from "react-hot-toast";

import {
  AnimatedPage,
  StaggerItem,
  StaggerList,
} from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import {
  downloadMySubscription,
  getMySubscriptions,
  type Subscription,
} from "@/api/enhanced";
import {
  copyText,
  formatBytes,
  formatDate,
  formatDateTime,
  saveTextFile,
} from "@/utils/enhanced";
import { getAdminFlag } from "@/utils/session";

type DownloadFormat = "clash" | "base64" | "uri";

export default function SubDownloadPage() {
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState<Subscription[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const isAdmin = getAdminFlag();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getMySubscriptions();

      if (res.code === 0 && res.data) setItems(res.data);
      else toast.error(res.msg || "加载订阅失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const fetchContent = async (item: Subscription, format: DownloadFormat) => {
    setBusy(`${item.id}-${format}`);
    try {
      const res = await downloadMySubscription(item.id, format);

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "获取订阅失败");

        return null;
      }

      return res.data;
    } finally {
      setBusy(null);
    }
  };

  const download = async (item: Subscription, format: DownloadFormat) => {
    const data = await fetchContent(item, format);

    if (data) {
      saveTextFile(data.content, data.fileName, data.mime);
      toast.success(`已下载 ${data.fileName}`);
    }
  };

  const copyLinks = async (item: Subscription) => {
    const data = await fetchContent(item, "uri");

    if (data) await copyText(data.content.trim(), "分享链接");
  };

  if (loading) {
    return <PageLoadingState message="正在加载订阅..." />;
  }

  const expired = (item: Subscription) =>
    item.expireTime > 0 && Date.now() > item.expireTime;

  return (
    <AnimatedPage className="px-3 lg:px-6 py-8 space-y-4">
      <Card className="border border-gray-200 dark:border-gray-700">
        <CardBody className="text-sm text-default-600 space-y-1">
          <p>
            订阅只提供文件下载，不提供公网订阅链接，配置不会暴露在公网。下载后在客户端中导入本地文件：
          </p>
          <ul className="list-disc pl-5 text-default-500 text-xs space-y-0.5">
            <li>
              Clash Verge Rev / Mihomo Party / FlClash：订阅或配置 → 新建 / 导入
              → 选择本地 YAML 文件
            </li>
            <li>
              v2rayN / Shadowrocket 等：使用 Base64
              文件或「复制分享链接」后从剪贴板导入
            </li>
            <li>
              SS 2022、mieru、Sudoku 节点需要 mihomo（Clash Meta）内核的客户端
            </li>
          </ul>
        </CardBody>
      </Card>

      {items.length === 0 ? (
        <Card className="shadow-sm border border-gray-200 dark:border-gray-700 bg-default-50/50">
          <CardBody className="text-center py-20 flex flex-col items-center justify-center min-h-[200px]">
            <h3 className="text-xl font-medium text-foreground tracking-tight mb-2">
              暂无可下载的订阅
            </h3>
            <p className="text-default-500 text-sm max-w-xs mx-auto leading-relaxed">
              {isAdmin
                ? "先在「订阅管理」中创建订阅。"
                : "请联系管理员为你分配订阅。"}
            </p>
          </CardBody>
        </Card>
      ) : (
        <StaggerList className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {items.map((item) => (
            <StaggerItem key={item.id}>
              <Card className="shadow-sm border border-gray-200 dark:border-gray-700 h-full">
                <CardHeader className="pb-2">
                  <div className="flex justify-between items-start w-full gap-2">
                    <div className="min-w-0">
                      <h3 className="font-semibold text-foreground truncate">
                        {item.name}
                      </h3>
                      {item.remark && (
                        <p className="text-xs text-default-500 mt-0.5">
                          {item.remark}
                        </p>
                      )}
                    </div>
                    {!item.enabled ? (
                      <Chip color="danger" size="sm" variant="flat">
                        已停用
                      </Chip>
                    ) : expired(item) ? (
                      <Chip color="warning" size="sm" variant="flat">
                        已过期
                      </Chip>
                    ) : (
                      <Chip color="success" size="sm" variant="flat">
                        可用
                      </Chip>
                    )}
                  </div>
                </CardHeader>
                <CardBody className="pt-0 space-y-3">
                  <div className="text-xs text-default-500 space-y-1">
                    <div>文件：{item.fileName}</div>
                    <div>
                      更新：
                      {formatDateTime(item.lastSyncTime || item.updatedTime)}
                    </div>
                    {item.subInfo && item.subInfo.total > 0 && (
                      <div>
                        流量：
                        {formatBytes(
                          item.subInfo.upload + item.subInfo.download,
                        )}{" "}
                        / {formatBytes(item.subInfo.total)}
                      </div>
                    )}
                    {item.subInfo?.expire ? (
                      <div>
                        套餐到期：{formatDate(item.subInfo.expire * 1000)}
                      </div>
                    ) : null}
                    {item.expireTime ? (
                      <div>下载截止：{formatDate(item.expireTime)}</div>
                    ) : null}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      color="primary"
                      isLoading={busy === `${item.id}-clash`}
                      size="sm"
                      variant="flat"
                      onPress={() => download(item, "clash")}
                    >
                      下载 Clash / mihomo 配置
                    </Button>
                    <Button
                      isLoading={busy === `${item.id}-base64`}
                      size="sm"
                      variant="flat"
                      onPress={() => download(item, "base64")}
                    >
                      下载 Base64
                    </Button>
                    <Button
                      isLoading={busy === `${item.id}-uri`}
                      size="sm"
                      variant="flat"
                      onPress={() => copyLinks(item)}
                    >
                      复制分享链接
                    </Button>
                  </div>
                </CardBody>
              </Card>
            </StaggerItem>
          ))}
        </StaggerList>
      )}
    </AnimatedPage>
  );
}
