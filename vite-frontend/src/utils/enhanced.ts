import toast from "react-hot-toast";

// 复制文本到剪贴板（兼容非 HTTPS 环境）
export const copyText = async (text: string, label = "内容") => {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      const textArea = document.createElement("textarea");

      textArea.value = text;
      textArea.style.position = "fixed";
      textArea.style.left = "-9999px";
      textArea.style.top = "0";
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      document.execCommand("copy");
      document.body.removeChild(textArea);
    }
    toast.success(`已复制${label}`);
  } catch {
    toast.error("复制失败");
  }
};

// 把文本保存为本地文件（订阅文件直接下载，不生成公网链接）
export const saveTextFile = (
  content: string,
  fileName: string,
  mime = "text/plain;charset=utf-8",
) => {
  const blob = new Blob([content], { type: mime });
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");

  link.href = url;
  link.setAttribute("download", fileName);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.setTimeout(() => window.URL.revokeObjectURL(url), 1000);
};

export const readFileText = (file: File) =>
  new Promise<string>((resolve, reject) => {
    const reader = new FileReader();

    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error);
    reader.readAsText(file, "utf-8");
  });

export const formatBytes = (bytes: number) => {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const index = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024)),
  );

  return `${(bytes / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 2)} ${units[index]}`;
};

export const formatDateTime = (ms: number) => {
  if (!ms) return "-";

  return new Date(ms).toLocaleString("zh-CN", { hour12: false });
};

export const formatDate = (ms: number) => {
  if (!ms) return "-";

  return new Date(ms).toLocaleDateString("zh-CN");
};

// 协议显示名
export const protocolLabel = (protocol: string) => {
  const map: Record<string, string> = {
    ss: "Shadowsocks",
    ssr: "SSR",
    vmess: "VMess",
    vless: "VLESS",
    trojan: "Trojan",
    hysteria: "Hysteria",
    hysteria2: "Hysteria2",
    tuic: "TUIC",
    anytls: "AnyTLS",
    mieru: "mieru",
    sudoku: "Sudoku",
    wireguard: "WireGuard",
    socks5: "SOCKS5",
    http: "HTTP",
    snell: "Snell",
    naive: "Naive",
  };

  return map[protocol] || protocol.toUpperCase();
};

export const sourceLabel = (source: string) => {
  switch (source) {
    case "manual":
      return "手动";
    case "import":
      return "导入";
    case "deploy":
      return "部署";
    case "relay":
      return "中转";
    default:
      return source || "-";
  }
};

export const hostPort = (host: string, port: number) => {
  if (!host) return "-";
  if (host.includes(":") && !host.startsWith("[")) {
    return `[${host}]:${port}`;
  }

  return `${host}:${port}`;
};

export const splitInputTags = (value: string) =>
  value
    .split(/[,，\s]+/)
    .map((item) => item.trim())
    .filter(Boolean);

// 单选下拉框的选中值；选回占位项（「全部…」）时为空字符串
export const firstKey = (keys: Set<React.Key>) =>
  String(Array.from(keys)[0] ?? "");
