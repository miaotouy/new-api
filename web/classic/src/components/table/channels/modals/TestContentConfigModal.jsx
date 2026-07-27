/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Button, Modal, Select, TextArea, Typography } from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../../helpers';

const TestContentConfigModal = ({
  visible,
  onCancel,
  channel,
  endpointType,
  isMobile,
  t,
}) => {
  const [config, setConfig] = React.useState(null);
  const [overrides, setOverrides] = React.useState({});
  const [selectedEndpoint, setSelectedEndpoint] = React.useState(
    endpointType || 'openai',
  );
  const [loading, setLoading] = React.useState(false);
  const [saving, setSaving] = React.useState(false);

  const endpoints = [
    { value: 'openai', label: 'OpenAI' },
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'gemini', label: 'Gemini' },
    { value: 'openai-response', label: 'OpenAI Response' },
    {
      value: 'openai-response-compact',
      label: 'OpenAI Response Compaction',
    },
    { value: 'embeddings', label: 'Embeddings' },
    { value: 'image-generation', label: t('图像生成') },
    { value: 'jina-rerank', label: 'Jina Rerank' },
  ];

  React.useEffect(() => {
    if (!visible || !channel) return;
    let cancelled = false;
    setLoading(true);
    API.get(`/api/channel/test/${channel.id}/config`)
      .then((res) => {
        if (cancelled) return;
        const data = res?.data?.data;
        if (!res?.data?.success || !data) {
          throw new Error(res?.data?.message || t('加载失败'));
        }
        setConfig(data);
        setOverrides(data.overrides || {});
        setSelectedEndpoint(endpointType || 'openai');
      })
      .catch((error) => {
        if (!cancelled) showError(error.message || t('加载失败'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [visible, channel?.id, endpointType, t]);

  const builtinOverride = config?.builtin_overrides?.[selectedEndpoint] || {};
  const savedOverride = overrides[selectedEndpoint];
  const effectiveOverride = savedOverride || builtinOverride;

  const updateOverride = (override) => {
    setOverrides((previous) => ({
      ...previous,
      [selectedEndpoint]: override,
    }));
  };

  const resetEndpoint = () => {
    setOverrides((previous) => {
      const next = { ...previous };
      delete next[selectedEndpoint];
      return next;
    });
  };

  const handleSave = async () => {
    if (!channel || !config) return;
    setSaving(true);
    try {
      const res = await API.put(`/api/channel/test/${channel.id}/config`, {
        version: config.version || 1,
        overrides,
      });
      if (!res?.data?.success) {
        throw new Error(res?.data?.message || t('保存失败'));
      }
      setConfig((previous) => ({ ...previous, overrides }));
      showSuccess(t('保存成功'));
    } catch (error) {
      showError(error.message || t('保存失败'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={`${t('配置')} ${t('内容')}`}
      visible={visible}
      onCancel={onCancel}
      confirmLoading={saving}
      onOk={handleSave}
      okText={t('保存')}
      cancelText={t('取消')}
      width={isMobile ? '100%' : 560}
    >
      {loading ? (
        <Typography.Text>{t('加载中...')}</Typography.Text>
      ) : (
        <div className='flex flex-col gap-3'>
          <Select
            value={selectedEndpoint}
            onChange={setSelectedEndpoint}
            optionList={endpoints}
            placeholder={t('选择端点类型')}
          />
          <Typography.Text type='tertiary'>
            {savedOverride ? t('配置') : t('默认')}
          </Typography.Text>
          {selectedEndpoint === 'jina-rerank' ? (
            <>
              <TextArea
                value={effectiveOverride.query || ''}
                onChange={(value) =>
                  updateOverride({
                    ...savedOverride,
                    query: value,
                    documents: effectiveOverride.documents || [''],
                  })
                }
                placeholder={t('查询')}
                maxLength={4096}
                autosize={{ minRows: 3, maxRows: 8 }}
              />
              {(effectiveOverride.documents || ['']).map(
                (document, index, documents) => (
                  <div key={index} className='flex gap-2'>
                    <TextArea
                      value={document}
                      onChange={(value) => {
                        const nextDocuments = [...documents];
                        nextDocuments[index] = value;
                        updateOverride({
                          ...savedOverride,
                          query: effectiveOverride.query || '',
                          documents: nextDocuments,
                        });
                      }}
                      placeholder={`${t('文档')} ${index + 1}`}
                      maxLength={4096}
                      autosize={{ minRows: 2, maxRows: 6 }}
                    />
                    <Button
                      type='danger'
                      theme='light'
                      onClick={() =>
                        updateOverride({
                          ...savedOverride,
                          query: effectiveOverride.query || '',
                          documents: documents.filter(
                            (_, documentIndex) => documentIndex !== index,
                          ),
                        })
                      }
                      disabled={documents.length <= 1}
                    >
                      -
                    </Button>
                  </div>
                ),
              )}
              <Button
                type='tertiary'
                onClick={() =>
                  updateOverride({
                    ...savedOverride,
                    query: effectiveOverride.query || '',
                    documents: [...(effectiveOverride.documents || ['']), ''],
                  })
                }
                disabled={(effectiveOverride.documents || ['']).length >= 8}
              >
                + {t('文档')}
              </Button>
            </>
          ) : (
            <TextArea
              value={
                effectiveOverride.content ||
                effectiveOverride.input ||
                effectiveOverride.prompt ||
                ''
              }
              onChange={(value) => {
                let field = 'content';
                if (selectedEndpoint === 'embeddings') field = 'input';
                if (selectedEndpoint === 'image-generation') field = 'prompt';
                updateOverride({ ...savedOverride, [field]: value });
              }}
              maxLength={4096}
              autosize={{ minRows: 4, maxRows: 10 }}
            />
          )}
          <Button type='tertiary' onClick={resetEndpoint}>
            {t('重置')}
          </Button>
        </div>
      )}
    </Modal>
  );
};

export default TestContentConfigModal;
