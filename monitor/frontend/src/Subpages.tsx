import { useRef, type ReactNode } from "react";

// Keep visited views mounted so changing navigation does not discard local forms or pagination.
export default function Subpages({
  activeKey,
  items,
}: {
  activeKey: string;
  items: { key: string; label: ReactNode; children?: ReactNode }[];
}) {
  const visited = useRef(new Set<string>());
  visited.current.add(activeKey);
  return (
    <>
      {items
        .filter((item) => visited.current.has(item.key))
        .map((item) => (
          <div key={item.key} hidden={item.key !== activeKey}>
            {item.children}
          </div>
        ))}
    </>
  );
}
