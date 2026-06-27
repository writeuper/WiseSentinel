import { MenuOutlined } from '@ant-design/icons';
import { Button, Tag } from 'antd';
import type { ReactNode } from 'react';
import { useOutletContext } from 'react-router-dom';

interface PageTopbarProps {
  title: string;
  extra?: ReactNode;
  tags?: string[];
}

interface OutletContext {
  openMobileNav?: () => void;
}

export default function PageTopbar({ title, extra, tags }: PageTopbarProps) {
  const ctx = useOutletContext<OutletContext>();

  return (
    <header className="page-topbar">
      <Button
        type="text"
        icon={<MenuOutlined />}
        className="show-mobile-only"
        onClick={ctx?.openMobileNav}
        style={{ display: 'none' }}
      />
      <h1>{title}</h1>
      {tags?.map((t) => (
        <Tag key={t} style={{ fontFamily: 'monospace', fontSize: 12 }}>
          {t}
        </Tag>
      ))}
      {extra}
      <style>{`
        @media (max-width: 768px) {
          .show-mobile-only { display: inline-flex !important; }
        }
      `}</style>
    </header>
  );
}
