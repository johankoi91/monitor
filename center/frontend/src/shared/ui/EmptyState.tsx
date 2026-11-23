import AntEmpty from "antd/es/empty";
import { type ReactNode } from "react";

export default function Empty({ children }: { children: ReactNode }) {
  return (
    <AntEmpty image={AntEmpty.PRESENTED_IMAGE_SIMPLE} description={children} />
  );
}
