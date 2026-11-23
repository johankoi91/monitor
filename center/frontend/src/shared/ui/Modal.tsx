import AntModal from "antd/es/modal";
import { type ReactNode } from "react";

export default function Modal({
  title,
  onClose,
  children,
  className,
  width,
}: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  className?: string;
  width?: number;
}) {
  return (
    <AntModal
      open
      title={title}
      className={className}
      width={width}
      footer={null}
      onCancel={onClose}
      destroyOnClose
    >
      {children}
    </AntModal>
  );
}
