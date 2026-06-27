import { DeleteOutlined, InboxOutlined, ReloadOutlined } from '@ant-design/icons';
import type { UploadProps } from 'antd';
import { Button, Popconfirm, Table, Tag, Upload, message } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import type { DocumentItem } from '@/api/types';
import { deleteDocument, getIndexTask, listDocuments, uploadDocument } from '@/api/client';
import PageTopbar from '@/components/PageTopbar';

const statusColor: Record<string, string> = {
  active: 'processing',
  success: 'success',
  running: 'processing',
  pending: 'default',
  failed: 'error',
  deleted: 'default',
};

export default function KnowledgePage() {
  const [docs, setDocs] = useState<DocumentItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listDocuments(1, 50);
      setDocs(data.items);
      setTotal(data.total);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const uploadProps: UploadProps = {
    name: 'file',
    multiple: false,
    showUploadList: false,
    accept: '.md,.txt,.markdown',
    customRequest: async ({ file, onSuccess, onError }) => {
      try {
        const f = file as File;
        const res = await uploadDocument(f);
        message.success(`上传成功：${res.file_name}，索引 ${res.status}`);
        if (res.task_id) {
          const task = await getIndexTask(res.task_id);
          message.info(`分块数：${task.chunk_count}`);
        }
        onSuccess?.(res);
        load();
      } catch (e) {
        message.error(e instanceof Error ? e.message : '上传失败');
        onError?.(e as Error);
      }
    },
  };

  const columns = [
    { title: '文档名称', dataIndex: 'name', key: 'name' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s: string) => <Tag color={statusColor[s] || 'default'}>{s}</Tag>,
    },
    { title: '可见性', dataIndex: 'visibility', key: 'visibility' },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at' },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, row: DocumentItem) => (
        <Popconfirm
          title="确认删除该文档？"
          onConfirm={async () => {
            try {
              await deleteDocument(row.doc_id);
              message.success('已删除');
              load();
            } catch (e) {
              message.error(e instanceof Error ? e.message : '删除失败');
            }
          }}
        >
          <Button size="small" danger icon={<DeleteOutlined />}>
            删除
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <div className="page-shell">
      <PageTopbar
        title="知识库"
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
              刷新
            </Button>
            <Upload {...uploadProps}>
              <Button type="primary">上传文档</Button>
            </Upload>
          </>
        }
      />

      <div className="page-body">
        <Upload.Dragger {...uploadProps} style={{ marginBottom: 24 }}>
          <p className="ant-upload-drag-icon">
            <InboxOutlined />
          </p>
          <p className="ant-upload-text">拖拽 Markdown / TXT 到此处，或点击上传</p>
          <p className="ant-upload-hint">支持 .md .txt .markdown · 最大 50MB</p>
        </Upload.Dragger>

        <Table
          rowKey="doc_id"
          columns={columns}
          dataSource={docs}
          loading={loading}
          pagination={{ total, pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        />
      </div>
    </div>
  );
}
