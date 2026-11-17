import Form from "antd/es/form";
import Input from "antd/es/input";
import Select from "antd/es/select";
import Switch from "antd/es/switch";
import Typography from "antd/es/typography";
import Alert from "antd/es/alert";
import InputNumber from "antd/es/input-number";

export interface MediaEdgeRange {enabled:boolean;host:string;start_port:number;end_port:number;}
export interface RTCParameters {
 media_edges?: MediaEdgeRange | null;
  app_id: string;
  auth_mode: string;
  channel_name?: string;
  channel_prefix: string;
  sender_uid: number;
  receiver_uid: number;
  private_deployment: boolean;
  access_ips?: string[];
  access_domains?: string[];
  verify_domain?: string;
  proxy_mode: number;
  disable_aut: boolean;
}
export const rtcDefaults: RTCParameters = {
  media_edges: {enabled:false,host:"111.230.192.59",start_port:4001,end_port:4008},
  app_id: "",
  auth_mode: "certificate",
  channel_prefix: "probe",
  sender_uid: 10001,
  receiver_uid: 10002,
  private_deployment: true,
  access_ips: ["111.230.192.59"],
  access_domains: ["111.230.192.59"],
  verify_domain: "ap.1145326.agora.local",
  proxy_mode: 0,
  disable_aut: true,
};

export default function RTCSettings({
  hasCertificate,
  hasSenderToken,
  hasReceiverToken,
}: {
  hasCertificate?: boolean;
  hasSenderToken?: boolean;
  hasReceiverToken?: boolean;
}) {
  const form=Form.useFormInstance();
  const edgeEnabled=Form.useWatch(["rtc","media_edges","enabled"]);
  const mode = Form.useWatch(["rtc", "auth_mode"]),
    privateDeployment = Form.useWatch(["rtc", "private_deployment"]),
    senderUID = Form.useWatch(["rtc", "sender_uid"]),
    receiverUID = Form.useWatch(["rtc", "receiver_uid"]);
  const listProps = {
    normalize: (v: string) => v.split(/[\s,，]+/).filter(Boolean),
    getValueProps: (v?: string[]) => ({ value: (v || []).join(", ") }),
  };
  return (
    <>
      <Alert
        type="info"
        showIcon
        message="固定 demo 音视频素材"
        description="一个探测服务内部运行发送/接收两个连接。固定使用 demo PCM 和 I420 文件；音频与视频均收到有效帧才通过。"
      />
      <Typography.Title level={5}>RTC 接入与鉴权</Typography.Title>
<Typography.Paragraph type="secondary">修改 AppID、鉴权方式、固定频道或 Token 绑定 UID 后，需要重新填写对应凭证。</Typography.Paragraph>
      <Form.Item
        label="Agora AppID"
        name={["rtc", "app_id"]}
        rules={[
          { required: true, message: "请填写 AppID" },
          {
            pattern: /^[a-fA-F0-9]{32}$/,
            message: "AppID 为 32 位十六进制标识",
          },
        ]}
      >
        <Input autoComplete="off" />
      </Form.Item>
      <Form.Item label="鉴权方式" name={["rtc", "auth_mode"]}>
        <Select
          options={[
            {
              value: "certificate",
              label: "中心使用 App Certificate 签发短期 Token",
            },
            { value: "token", label: "填写预签发 Token" },
            { value: "none", label: "项目未启用 Token 鉴权" },
          ]}
        />
      </Form.Item>
      {mode === "certificate" && (
        <Form.Item
          label="App Certificate"
          name={["credentials", "app_certificate"]}
          extra={
            hasCertificate
              ? "已设置；留空保留原值。凭证不回显，只在中心加密保存。"
              : "中心加密保存，不下发 App Certificate 给探针。"
          }
          rules={[
            {
              validator: (_, v) =>
                (hasCertificate && !v) || /^[a-fA-F0-9]{32}$/.test(v || "")
                  ? Promise.resolve()
                  : Promise.reject(new Error("请填写 32 位 App Certificate")),
            },
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      )}
      <div className="rtc-form-grid">
        <Form.Item label="发送端 UID" name={["rtc","sender_uid"]} rules={[{required:true}]}><InputNumber min={1} max={4294967295} precision={0}/></Form.Item>
        <Form.Item label="接收端 UID" name={["rtc","receiver_uid"]} dependencies={[["rtc","sender_uid"]]} rules={[{required:true},({getFieldValue})=>({validator:(_,v)=>v!==getFieldValue(["rtc","sender_uid"])?Promise.resolve():Promise.reject(new Error("发送和接收 UID 须不同"))})]}><InputNumber min={1} max={4294967295} precision={0}/></Form.Item>
      </div>
      {mode === "token" && (
        <>
          <Form.Item
            label="固定测试频道"
            name={["rtc", "channel_name"]}
            rules={[{ required: true, message: "请填写 Token 绑定频道" }]}
          >
            <Input maxLength={64} />
          </Form.Item>
          <Form.Item
            label={`发送端 Token（UID ${senderUID || 10001}）`}
            name={["credentials", "sender_token"]}
            extra={
              hasSenderToken
                ? "已设置，留空保留"
                : `需与固定频道及 UID ${senderUID || 10001} 匹配`
            }
            rules={[
              {
                validator: (_, v) =>
                  hasSenderToken || v
                    ? Promise.resolve()
                    : Promise.reject(new Error("请填写发送端 Token")),
              },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            label={`接收端 Token（UID ${receiverUID || 10002}）`}
            name={["credentials", "receiver_token"]}
            extra={
              hasReceiverToken
                ? "已设置，留空保留"
                : `需与固定频道及 UID ${receiverUID || 10002} 匹配`
            }
            rules={[
              {
                validator: (_, v) =>
                  hasReceiverToken || v
                    ? Promise.resolve()
                    : Promise.reject(new Error("请填写接收端 Token")),
              },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </>
      )}
      {mode !== "token" && (
        <Form.Item
          label="专用频道前缀"
          name={["rtc", "channel_prefix"]}
          extra="每轮自动生成独立频道；关联此目标的任务共用这里配置的发送和接收 UID。"
        >
          <Input maxLength={24} />
        </Form.Item>
      )}
      <Typography.Title level={5}>UDP media edge 逐端口探测</Typography.Title>
      <Form.Item label="启用逐端口探测" name={["rtc","media_edges","enabled"]} valuePropName="checked">
        <Switch onChange={enabled=>{if(enabled){const current=form.getFieldValue(["rtc","media_edges"])||{};form.setFieldValue(["rtc","media_edges"],{enabled:true,host:current.host||form.getFieldValue(["rtc","access_ips"])?.[0]||"111.230.192.59",start_port:current.start_port||4001,end_port:current.end_port||4008});}}}/>
      </Form.Item>
      {edgeEnabled && <>
        <Typography.Paragraph type="secondary">每次任务按端口从小到大逐个完成音视频收发，全部通过才算本轮通过。端口失败后继续检查后续端口；未完成整轮时不重入。创建任务关联此目标时，可按端口数量配置周期和单端口超时。</Typography.Paragraph>
        <Form.Item label="UDP edge IP / 主机名" name={["rtc","media_edges","host"]} rules={[{required:true,message:"填写 media edge 所在主机"}]}><Input placeholder="111.230.192.59"/></Form.Item>
        <div className="rtc-form-grid">
          <Form.Item label="起始 UDP 端口" name={["rtc","media_edges","start_port"]} rules={[{required:true}]}><InputNumber min={1} max={65535} precision={0}/></Form.Item>
          <Form.Item label="结束 UDP 端口" name={["rtc","media_edges","end_port"]} dependencies={[["rtc","media_edges","start_port"]]} rules={[{required:true},({getFieldValue})=>({validator:(_,value)=>value>=getFieldValue(["rtc","media_edges","start_port"]) && value-getFieldValue(["rtc","media_edges","start_port"])<64?Promise.resolve():Promise.reject(new Error("结束端口不得小于起始端口，一轮最多 64 个端口"))})]}><InputNumber min={1} max={65535} precision={0}/></Form.Item>
        </div>
      </>}
      <Typography.Title level={5}>私有化部署</Typography.Title>
      <Form.Item
        label="连接私有化部署"
        name={["rtc", "private_deployment"]}
        valuePropName="checked"
      >
        <Switch />
      </Form.Item>
      {privateDeployment && (
        <>
          <Form.Item
            label="接入 IP 列表"
            name={["rtc", "access_ips"]}
            {...listProps}
            extra="多个地址用逗号或空格分隔；IP 和域名至少填写一项。"
          >
            <Input />
          </Form.Item>
          <Form.Item
            label="接入域名列表"
            name={["rtc", "access_domains"]}
            {...listProps}
          >
            <Input />
          </Form.Item>
          <Form.Item label="证书校验域名" name={["rtc", "verify_domain"]}>
            <Input />
          </Form.Item>
          <Form.Item label="接入路径策略" name={["rtc", "proxy_mode"]}>
            <Select
              options={[
                { value: 0, label: "优先私有化接入（ConnectivityFirst）" },
                { value: 1, label: "仅连接配置的私有化接入点" },
              ]}
            />
          </Form.Item>
          <Form.Item
            label="禁用 vos-aut"
            name={["rtc", "disable_aut"]}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        </>
      )}
    </>
  );
}
