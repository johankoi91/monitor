import { InfoCircleOutlined } from "@ant-design/icons";
import type { ReactNode } from "react";

export function RTCSectionHeading({
  title,
  description,
  icon,
  extra,
}: {
  title: string;
  description?: string;
  icon?: ReactNode;
  extra?: ReactNode;
}) {
  return (
    <div className="rtc-section-heading">
      <div className="rtc-section-heading-main">
        {icon && (
          <span className="rtc-section-icon" aria-hidden>
            {icon}
          </span>
        )}
        <div>
          <h3>{title}</h3>
          {description && <p>{description}</p>}
        </div>
      </div>
      {extra}
    </div>
  );
}
export function RTCDialogIntro({ children }: { children: ReactNode }) {
  return (
    <div className="rtc-dialog-intro">
      <InfoCircleOutlined aria-hidden />
      <p>{children}</p>
    </div>
  );
}
