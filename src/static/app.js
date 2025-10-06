(() => {
    const state = {
        API_BASE: '',
        collections: [],
        currentCollection: null,
        currentPage: 1,
        limit: 20,
        content: null,
        schema: null,
        conditionCounter: 0,
        showingQuery: false,
    };

    const refs = {};

    document.addEventListener('DOMContentLoaded', () => {
        cacheElements();
        bindEvents();
        updateQueryPreview();
        loadCollections();
        checkServerStatus();
    });

    function cacheElements() {
        refs.toast = document.getElementById('toast');
        refs.statusIndicator = document.getElementById('status-indicator');
        refs.statusText = document.getElementById('status-text');
        refs.collectionList = document.getElementById('collection-list');
        refs.refreshCollections = document.getElementById('refresh-collections');
        refs.deleteCollection = document.getElementById('delete-collection');
        refs.openCreateCollection = document.getElementById('open-create-collection');
        refs.openAddDocument = document.getElementById('open-add-document');
        refs.advancedCollection = document.getElementById('advanced-collection');
        refs.resultLimit = document.getElementById('result-limit');
        refs.resultOffset = document.getElementById('result-offset');
        refs.queryBuilder = document.getElementById('query-builder');
        refs.addCondition = document.getElementById('add-condition');
        refs.executeQuery = document.getElementById('execute-query');
        refs.clearQuery = document.getElementById('clear-query');
        refs.contentTitle = document.getElementById('content-title');
        refs.collectionInfo = document.getElementById('collection-info');
        refs.contentTableContainer = document.getElementById('content-table-container');
        refs.pagination = document.getElementById('pagination');
        refs.queryPreview = document.getElementById('query-json-preview');
        refs.modalAddDocument = document.getElementById('modal-add-document');
        refs.modalEditDocument = document.getElementById('modal-edit-document');
        refs.modalCreateCollection = document.getElementById('modal-create-collection');
        refs.addFields = document.getElementById('add-fields');
        refs.editFields = document.getElementById('edit-fields');
        refs.addDocumentForm = document.getElementById('add-document-form');
        refs.editDocumentForm = document.getElementById('edit-document-form');
        refs.createCollectionForm = document.getElementById('create-collection-form');
        refs.addFieldRowButton = document.getElementById('add-field-row');
        refs.fieldsContainer = document.getElementById('fields-container');
        resetCreateCollectionForm();
    }

    function bindEvents() {
        refs.collectionList.addEventListener('click', handleCollectionClick);
        refs.refreshCollections.addEventListener('click', loadCollections);
        refs.deleteCollection.addEventListener('click', handleDeleteCollection);
        refs.openCreateCollection.addEventListener('click', () => toggleModal(refs.modalCreateCollection, true));
        refs.openAddDocument.addEventListener('click', handleOpenAddDocument);
        refs.addCondition.addEventListener('click', () => addCondition());
        refs.executeQuery.addEventListener('click', executeQuery);
        refs.clearQuery.addEventListener('click', clearQuery);
        refs.queryBuilder.addEventListener('change', updateQueryPreview);
        refs.queryBuilder.addEventListener('input', updateQueryPreview);
        refs.queryBuilder.addEventListener('click', handleConditionAction);
        refs.resultLimit.addEventListener('input', updateQueryPreview);
        refs.resultOffset.addEventListener('input', updateQueryPreview);
        refs.advancedCollection.addEventListener('change', handleAdvancedCollectionChange);
        refs.contentTableContainer.addEventListener('click', handleTableAction);
        refs.addDocumentForm.addEventListener('submit', submitAddDocument);
        refs.editDocumentForm.addEventListener('submit', submitEditDocument);
        refs.createCollectionForm.addEventListener('submit', submitCreateCollection);
        refs.addFieldRowButton.addEventListener('click', () => addFieldRow());
        document.body.addEventListener('click', handleGlobalClick, true);
    }

    function handleGlobalClick(event) {
        const target = event.target.closest('[data-close]');
        if (target) {
            const id = target.getAttribute('data-close');
            const modal = document.getElementById(id);
            if (modal) {
                toggleModal(modal, false);
            }
            return;
        }

        if (event.target.classList.contains('modal')) {
            toggleModal(event.target, false);
        }
    }

    async function loadCollections() {
        try {
            const result = await apiCall('/collections/list');
            state.collections = result.collections || [];
            renderCollections();
            updateAdvancedCollectionOptions();
            showToast(`載入 ${state.collections.length} 個 Collections`);
        } catch (error) {
            renderCollections([]);
            updateAdvancedCollectionOptions();
            showToast(`載入 Collections 失敗：${error.message}`, true);
        } finally {
            checkServerStatus();
        }
    }

    function renderCollections(list = state.collections) {
        if (!list.length) {
            refs.collectionList.innerHTML = '<li class="collection-list__item collection-list__item--empty">暫無資料</li>';
            return;
        }

        refs.collectionList.innerHTML = list.map(collection => {
            const activeClass = collection.name === state.currentCollection ? 'collection-list__item--active' : '';
            const count = collection.document_count ?? '–';
            return `
                <li class="collection-list__item ${activeClass}" data-collection="${collection.name}">
                    <span>${collection.name}</span>
                    <span class="collection-list__meta">${count}</span>
                </li>
            `;
        }).join('');
    }

    function handleCollectionClick(event) {
        const item = event.target.closest('[data-collection]');
        if (!item) return;
        const name = item.getAttribute('data-collection');
        selectCollection(name);
    }

    function selectCollection(name) {
        if (!name) return;
        state.currentCollection = name;
        state.currentPage = 1;
        state.showingQuery = false;
        renderCollections();
        refs.openAddDocument.disabled = false;
        updateAdvancedCollectionOptions();
        loadCollectionContent(name, 1);
        showToast(`已選擇 Collection：${name}`);
    }

    async function loadCollectionContent(collectionName = state.currentCollection, page = state.currentPage) {
        if (!collectionName) return;
        try {
            const url = `/collections/content?collection=${encodeURIComponent(collectionName)}&page=${page}&limit=${state.limit}`;
            const result = await apiCall(url);
            state.content = result;
            state.currentPage = result.pagination?.page || page;
            state.limit = result.pagination?.limit || state.limit;
            state.schema = parseSchema(result.schema, collectionName);
            renderCollectionContent(result);
        } catch (error) {
            state.content = null;
            state.schema = null;
            renderEmptyContent(error.message);
            showToast(`載入內容失敗：${error.message}`, true);
        }
    }

    function parseSchema(schemaText, collectionName) {
        if (!schemaText) return null;
        try {
            return JSON.parse(schemaText);
        } catch (error) {
            console.warn('Unable to parse schema for', collectionName, error);
            return null;
        }
    }

    function renderCollectionContent(content) {
        const columns = content.columns || inferColumns(content.records);
        const pagination = content.pagination || { page: 1, total_pages: 1, total_count: content.records?.length || 0 };
        const collectionName = content.collection || state.currentCollection || '未命名';

        refs.contentTitle.textContent = `${collectionName} - 內容`;
        refs.collectionInfo.textContent = `共 ${pagination.total_count} 筆資料，頁數 ${pagination.page}/${pagination.total_pages}`;
        refs.openAddDocument.disabled = false;

        renderTable(columns, content.records || []);
        renderPagination(pagination);
    }

    function renderEmptyContent(message) {
        refs.contentTitle.textContent = 'Collection 內容';
        refs.collectionInfo.textContent = message || '請先選擇 Collection';
        refs.openAddDocument.disabled = !state.currentCollection;
        refs.contentTableContainer.innerHTML = `
            <div class="placeholder">
                <h3>${message ? '載入失敗' : '未選擇 Collection'}</h3>
                <p>${message || '請從左側列表選擇 Collection'}</p>
            </div>
        `;
        refs.pagination.innerHTML = '';
    }

    function renderTable(columns, records) {
        if (!records.length) {
            refs.contentTableContainer.innerHTML = `
                <div class="placeholder">
                    <h3>沒有資料</h3>
                    <p>目前沒有可以顯示的文檔。</p>
                </div>
            `;
            return;
        }

        const primaryKey = state.schema?.primary_key || columns[0];
        const thead = `
            <thead>
                <tr>
                    ${columns.map(col => `<th>${col}</th>`).join('')}
                    <th style="width: 80px;">操作</th>
                </tr>
            </thead>
        `;

        const tbody = `
            <tbody>
                ${records.map((record, index) => `
                    <tr data-row="${index}" data-id="${record[primaryKey] ?? ''}">
                        ${columns.map(col => `
                            <td>
                                <div class="cell-content" title="點擊展開" data-action="toggle-cell">
                                    ${formatCell(record[col])}
                                </div>
                            </td>
                        `).join('')}
                        <td>
                            <div class="table-actions">
                                <button type="button" class="ghost" data-action="edit" data-index="${index}">編輯</button>
                                <button type="button" class="ghost" data-action="delete" data-id="${record[primaryKey] ?? ''}">刪除</button>
                            </div>
                        </td>
                    </tr>
                `).join('')}
            </tbody>
        `;

        refs.contentTableContainer.innerHTML = `<div class="table-wrapper"><table class="data-table">${thead}${tbody}</table></div>`;
    }

    function renderPagination(pagination) {
        if (!pagination || pagination.total_pages <= 1) {
            refs.pagination.innerHTML = '';
            return;
        }

        refs.pagination.innerHTML = `
            <span>第 ${pagination.page} / ${pagination.total_pages} 頁，總筆數 ${pagination.total_count}</span>
            <div class="pagination__controls">
                <button type="button" class="ghost" data-page="prev" ${pagination.page <= 1 ? 'disabled' : ''}>上一頁</button>
                <button type="button" class="ghost" data-page="next" ${pagination.page >= pagination.total_pages ? 'disabled' : ''}>下一頁</button>
            </div>
        `;

        refs.pagination.querySelectorAll('button').forEach(btn => {
            btn.addEventListener('click', handlePaginationClick);
        });
    }

    function handlePaginationClick(event) {
        const { page } = event.currentTarget.dataset;
        const pagination = state.content?.pagination;
        if (!pagination) return;
        let targetPage = pagination.page;
        if (page === 'prev' && pagination.page > 1) targetPage = pagination.page - 1;
        if (page === 'next' && pagination.page < pagination.total_pages) targetPage = pagination.page + 1;
        if (targetPage === pagination.page) return;
        loadCollectionContent(state.currentCollection, targetPage);
    }

    function handleTableAction(event) {
        const actionButton = event.target.closest('[data-action]');
        if (!actionButton) return;
        const action = actionButton.getAttribute('data-action');

        if (action === 'toggle-cell') {
            event.target.classList.toggle('is-expanded');
            return;
        }

        if (action === 'edit') {
            const index = Number(actionButton.getAttribute('data-index'));
            const record = state.content?.records?.[index];
            if (record) openEditDocument(record);
            return;
        }

        if (action === 'delete') {
            const id = actionButton.getAttribute('data-id');
            deleteRecord(id);
        }
    }

    function handleAdvancedCollectionChange() {
        updateQueryPreview();
        const conditions = refs.queryBuilder.querySelectorAll('.query-condition');
        conditions.forEach(condition => refreshConditionOptions(condition));
    }

    function handleConditionAction(event) {
        const removeBtn = event.target.closest('[data-role="remove-condition"]');
        if (removeBtn) {
            const condition = removeBtn.closest('.query-condition');
            condition?.remove();
            updateQueryPreview();
            return;
        }

        const typeSelect = event.target.closest('[data-role="condition-type"]');
        if (typeSelect) {
            const condition = typeSelect.closest('.query-condition');
            renderConditionFields(condition, typeSelect.value);
            updateQueryPreview();
        }
    }

    function addCondition(type = 'search') {
        state.conditionCounter += 1;
        const id = `condition-${state.conditionCounter}`;
        const element = document.createElement('div');
        element.className = 'query-condition';
        element.dataset.condition = id;
        element.innerHTML = `
            <select data-role="condition-type">
                <option value="search" ${type === 'search' ? 'selected' : ''}>search</option>
                <option value="sql" ${type === 'sql' ? 'selected' : ''}>sql</option>
            </select>
            <div class="query-condition__fields"></div>
            <button type="button" class="ghost" data-role="remove-condition" title="移除">✕</button>
        `;
        refs.queryBuilder.appendChild(element);
        renderConditionFields(element, type);
        updateQueryPreview();
    }

    function renderConditionFields(condition, type) {
        const fieldsContainer = condition.querySelector('.query-condition__fields');
        if (!fieldsContainer) return;

        if (type === 'search') {
            fieldsContainer.innerHTML = `
                <select data-role="search-fields"></select>
                <input type="text" placeholder="關鍵字" data-role="search-term">
                <select data-role="search-operator">
                    <option value="AND">AND</option>
                    <option value="OR">OR</option>
                </select>
            `;
            populateSearchFields(fieldsContainer.querySelector('[data-role="search-fields"]'));
        } else {
            fieldsContainer.innerHTML = `
                <select data-role="sql-field"></select>
                <select data-role="sql-operator">
                    <option value="=">=</option>
                    <option value="!=">!=</option>
                    <option value=">">></option>
                    <option value=">=">>=</option>
                    <option value="<"><</option>
                    <option value="<="><=</option>
                    <option value="LIKE">LIKE</option>
                </select>
                <input type="text" placeholder="值" data-role="sql-value">
            `;
            populateSqlFields(fieldsContainer.querySelector('[data-role="sql-field"]'));
        }
    }

    async function populateSearchFields(selectElement) {
        const fields = await fetchSchemaFields('text');
        selectElement.innerHTML = '<option value="">所有字段</option>' +
            fields.map(field => `<option value="${field.name}">${field.name} (${field.type})</option>`).join('');
    }

    async function populateSqlFields(selectElement) {
        const fields = await fetchSchemaFields();
        selectElement.innerHTML = '<option value="">選擇字段</option>' +
            fields.map(field => `<option value="${field.name}">${field.name} (${field.type})</option>`).join('');
    }

    async function fetchSchemaFields(filterType) {
        if (!state.currentCollection) return [];
        if (!state.schema) {
            await loadCollectionContent(state.currentCollection, state.currentPage);
        }
        const schema = state.schema;
        if (!schema?.fields) return [];
        return schema.fields.filter(field => !filterType || field.type === filterType);
    }

    function refreshConditionOptions(condition) {
        const type = condition.querySelector('[data-role="condition-type"]').value;
        renderConditionFields(condition, type);
    }

    function updateQueryPreview() {
        const collection = refs.advancedCollection.value || '';
        const limit = Number(refs.resultLimit.value) || 20;
        const offset = Number(refs.resultOffset.value) || 0;
        const conditions = Array.from(refs.queryBuilder.querySelectorAll('.query-condition'));
        const queryParts = [];

        conditions.forEach(condition => {
            const type = condition.querySelector('[data-role="condition-type"]').value;
            if (type === 'search') {
                const term = condition.querySelector('[data-role="search-term"]').value.trim();
                const operator = condition.querySelector('[data-role="search-operator"]').value || 'AND';
                const field = condition.querySelector('[data-role="search-fields"]').value;
                if (!term) return;
                const searchObj = { term, operator };
                if (field) searchObj.fields = [field];
                queryParts.push({ search: searchObj });
            } else {
                const field = condition.querySelector('[data-role="sql-field"]').value;
                const operator = condition.querySelector('[data-role="sql-operator"]').value || '=';
                const value = condition.querySelector('[data-role="sql-value"]').value.trim();
                if (!field || !value) return;
                const operatorMap = {
                    '=': '$eq',
                    '!=': '$ne',
                    '>': '$gt',
                    '>=': '$gte',
                    '<': '$lt',
                    '<=': '$lte',
                    'LIKE': '$like',
                };
                const mapped = operatorMap[operator] || '$eq';
                const where = {};
                if (mapped === '$like') {
                    where[field] = { '$like': `%${value}%` };
                } else {
                    where[field] = { [mapped]: value };
                }
                queryParts.push({ sql: { where } });
            }
        });

        let query = {};
        if (queryParts.length === 1) {
            query = queryParts[0];
        } else if (queryParts.length > 1) {
            query = { '$and': queryParts };
        }

        const request = {
            collection,
            query,
            result: {
                limit,
                offset,
                order_by: [{ field: '_score', direction: 'desc' }],
            },
        };

        refs.queryPreview.textContent = JSON.stringify(request, null, 2);
    }

    async function executeQuery() {
        updateQueryPreview();
        try {
            const payload = JSON.parse(refs.queryPreview.textContent);
            if (!payload.collection) {
                showToast('請先選擇 Collection', true);
                return;
            }
            const result = await apiCall('/query', 'POST', payload);
            displayQueryResult(payload.collection, result);
            showToast('進階查詢完成');
        } catch (error) {
            showToast(`查詢失敗：${error.message}`, true);
        }
    }

    function displayQueryResult(collectionName, result) {
        state.showingQuery = true;
        const records = Array.isArray(result.records) ? result.records : (result.records ? [result.records] : []);
        const columns = result.columns || inferColumns(records);
        const total = result.pagination?.total_count ?? records.length;
        const pagination = result.pagination || { page: 1, total_pages: 1, total_count: total };

        refs.contentTitle.textContent = `查詢結果 - ${collectionName}`;
        refs.collectionInfo.textContent = `共 ${pagination.total_count} 筆符合條件的資料`;
        refs.openAddDocument.disabled = true;

        if (!records.length) {
            refs.contentTableContainer.innerHTML = `
                <div class="placeholder">
                    <h3>查無資料</h3>
                    <p>沒有符合條件的結果。</p>
                </div>
            `;
            refs.pagination.innerHTML = '';
            return;
        }

        renderTable(columns, records);
        refs.pagination.innerHTML = '';
    }

    function clearQuery() {
        refs.queryBuilder.innerHTML = '';
        state.conditionCounter = 0;
        refs.resultLimit.value = 20;
        refs.resultOffset.value = 0;
        updateQueryPreview();
        if (state.currentCollection) {
            loadCollectionContent(state.currentCollection, 1);
        } else {
            renderEmptyContent('請先選擇 Collection');
        }
        showToast('查詢條件已清除');
    }

    function formatCell(value) {
        if (value === null || value === undefined) return '<span style="color:#888">null</span>';
        const str = String(value);
        if (str.length > 80) {
            return `${str.slice(0, 80)}…`;
        }
        return escapeHtml(str);
    }

    function inferColumns(records) {
        if (!records || !records.length) return [];
        return Object.keys(records[0]).filter(key => !key.startsWith('_'));
    }

    function escapeHtml(str) {
        return str
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    function toggleModal(modal, visible) {
        if (!modal) return;
        modal.setAttribute('aria-hidden', visible ? 'false' : 'true');
    }

    function handleOpenAddDocument() {
        if (!state.currentCollection) {
            showToast('請先選擇 Collection', true);
            return;
        }
        if (!state.schema) {
            showToast('尚未取得 schema', true);
            return;
        }
        buildDocumentFields(refs.addFields, state.schema.fields, {});
        toggleModal(refs.modalAddDocument, true);
    }

    function openEditDocument(record) {
        if (!state.schema) {
            showToast('尚未取得 schema', true);
            return;
        }
        buildDocumentFields(refs.editFields, state.schema.fields, record, true, state.schema.primary_key);
        toggleModal(refs.modalEditDocument, true);
    }

    function buildDocumentFields(container, fields, record = {}, isEdit = false, primaryKey) {
        container.innerHTML = '';
        if (!Array.isArray(fields) || !fields.length) {
            container.innerHTML = '<p class="section-subtitle">缺少 schema 定義。</p>';
            return;
        }

        fields.forEach(field => {
            const wrapper = document.createElement('label');
            wrapper.className = 'form-field';
            const isPrimary = primaryKey && field.name === primaryKey;
            wrapper.innerHTML = `
                <span>${field.name} (${field.type})${isPrimary ? ' *' : ''}</span>
            `;
            const input = createFieldInput(field, record[field.name]);
            input.name = field.name;
            input.required = true;
            if (isEdit && isPrimary) {
                input.readOnly = true;
                input.classList.add('is-readonly');
                input.required = false;
            }
            wrapper.appendChild(input);
            container.appendChild(wrapper);
        });
    }

    function createFieldInput(field, value) {
        let element;
        if (field.type === 'text' && (field.name.includes('content') || field.name.includes('description'))) {
            element = document.createElement('textarea');
        } else if (field.type === 'integer' || field.type === 'real') {
            element = document.createElement('input');
            element.type = 'number';
            element.step = field.type === 'integer' ? '1' : 'any';
        } else {
            element = document.createElement('input');
            element.type = 'text';
        }
        element.value = value ?? '';
        return element;
    }

    async function submitAddDocument(event) {
        event.preventDefault();
        try {
            const documentData = collectDocumentData(refs.addFields, state.schema);
            await apiCall('/documents/upsert', 'POST', {
                collection: state.currentCollection,
                document: documentData,
            });
            toggleModal(refs.modalAddDocument, false);
            showToast('文檔新增成功');
            loadCollectionContent(state.currentCollection, 1);
        } catch (error) {
            showToast(`新增文檔失敗：${error.message}`, true);
        }
    }

    async function submitEditDocument(event) {
        event.preventDefault();
        try {
            const documentData = collectDocumentData(refs.editFields, state.schema, true);
            await apiCall('/documents/upsert', 'POST', {
                collection: state.currentCollection,
                document: documentData,
            });
            toggleModal(refs.modalEditDocument, false);
            showToast('文檔更新成功');
            loadCollectionContent(state.currentCollection, state.currentPage);
        } catch (error) {
            showToast(`更新文檔失敗：${error.message}`, true);
        }
    }

    function collectDocumentData(container, schema, preservePrimary) {
        if (!schema?.fields) throw new Error('缺少 schema');
        const data = {};
        const primaryKey = schema.primary_key;

        schema.fields.forEach(field => {
            const input = container.querySelector(`[name="${field.name}"]`);
            if (!input) return;
            const rawValue = input.value;
            const value = rawValue.trim();

            if (!value) {
                if (preservePrimary && field.name === primaryKey) {
                    data[field.name] = rawValue;
                    return;
                }
                throw new Error(`${field.name} 不能為空`);
            }

            if (field.type === 'integer') {
                const parsed = parseInt(value, 10);
                if (Number.isNaN(parsed)) throw new Error(`${field.name} 必須是整數`);
                data[field.name] = parsed;
                return;
            }
            if (field.type === 'real') {
                const parsed = parseFloat(value);
                if (Number.isNaN(parsed)) throw new Error(`${field.name} 必須是數字`);
                data[field.name] = parsed;
                return;
            }
            data[field.name] = value;
        });

        return data;
    }

    async function deleteRecord(id) {
        if (!state.currentCollection || !id) {
            showToast('缺少刪除所需資訊', true);
            return;
        }

        const confirmDelete = window.confirm(`確定刪除 ID 為 ${id} 的文檔？`);
        if (!confirmDelete) return;

        try {
            await apiCall('/documents/delete', 'POST', {
                collection: state.currentCollection,
                id,
            });
            showToast('文檔刪除成功');
            loadCollectionContent(state.currentCollection, state.currentPage);
        } catch (error) {
            showToast(`刪除失敗：${error.message}`, true);
        }
    }

    async function submitCreateCollection(event) {
        event.preventDefault();
        try {
            const name = document.getElementById('new-collection-name').value.trim();
            const primaryKey = document.getElementById('new-primary-key').value.trim();
            const stemming = document.getElementById('fts-stemming').checked;

            if (!name || !primaryKey) {
                showToast('請填寫必要欄位', true);
                return;
            }

            const fields = Array.from(refs.fieldsContainer.querySelectorAll('.field-row')).map(row => {
                const fieldName = row.querySelector('[data-field="name"]').value.trim();
                const fieldType = row.querySelector('[data-field="type"]').value;
                const fieldWeight = row.querySelector('[data-field="weight"]').value;
                const indexed = row.querySelector('[data-field="indexed"]').checked;

                if (!fieldName) throw new Error('字段名稱不可為空');

                const field = { name: fieldName, type: fieldType, indexed };
                if (fieldWeight) {
                    const weightVal = parseFloat(fieldWeight);
                    if (!Number.isNaN(weightVal)) field.weight = weightVal;
                }
                return field;
            });

            if (!fields.length) {
                showToast('至少需要一個字段', true);
                return;
            }

            const schema = {
                name,
                primary_key: primaryKey,
                fts: { stemming },
                fields,
            };

            await apiCall('/collections/create', 'POST', schema);
            toggleModal(refs.modalCreateCollection, false);
            showToast('Collection 建立成功');
            loadCollections();
        } catch (error) {
            showToast(`建立失敗：${error.message}`, true);
        }
    }

    async function handleDeleteCollection() {
        if (!state.currentCollection) {
            showToast('請先選擇 Collection', true);
            return;
        }

        const confirmDelete = window.confirm(`確定刪除 Collection 「${state.currentCollection}」？`);
        if (!confirmDelete) return;

        try {
            await apiCall('/collections/delete', 'POST', { name: state.currentCollection });
            showToast('Collection 刪除成功');
            state.currentCollection = null;
            state.content = null;
            state.schema = null;
            refs.openAddDocument.disabled = true;
            renderCollections();
            renderEmptyContent('請選擇 Collection');
            loadCollections();
        } catch (error) {
            showToast(`刪除失敗：${error.message}`, true);
        }
    }

    function addFieldRow(name = '', type = 'text', weight = '', indexed = true) {
        const row = document.createElement('div');
        row.className = 'field-row';
        row.innerHTML = `
            <input type="text" placeholder="字段名稱" value="${name}" data-field="name" required>
            <select data-field="type">
                <option value="text" ${type === 'text' ? 'selected' : ''}>text</option>
                <option value="integer" ${type === 'integer' ? 'selected' : ''}>integer</option>
                <option value="real" ${type === 'real' ? 'selected' : ''}>real</option>
            </select>
            <input type="number" step="0.1" min="0" placeholder="權重" value="${weight}" data-field="weight">
            <label class="form-field form-field--inline" style="margin:0;">
                <input type="checkbox" data-field="indexed" ${indexed ? 'checked' : ''}>
                <span>索引</span>
            </label>
            <button type="button" class="ghost" title="移除" aria-label="移除字段">✕</button>
        `;
        row.querySelector('button').addEventListener('click', () => row.remove());
        refs.fieldsContainer.appendChild(row);
    }

    function resetCreateCollectionForm() {
        refs.fieldsContainer.innerHTML = '';
        addFieldRow('id', 'text', '', true);
        addFieldRow('title', 'text', '2', true);
        addFieldRow('content', 'text', '1', true);
        addFieldRow('created_at', 'integer', '', true);
    }

    async function apiCall(endpoint, method = 'GET', data) {
        const options = {
            method,
            headers: {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-cache',
            },
        };

        if (data) {
            options.body = JSON.stringify(data);
        }

        const separator = endpoint.includes('?') ? '&' : '?';
        const response = await fetch(`${state.API_BASE}${endpoint}${separator}_t=${Date.now()}`, options);
        const text = await response.text();

        let json;
        try {
            json = JSON.parse(text);
        } catch (error) {
            json = text;
        }

        if (!response.ok) {
            const message = json?.error || json?.message || response.statusText;
            throw new Error(message);
        }

        return json;
    }

    function showToast(message, isError = false) {
        if (!refs.toast) return;
        refs.toast.textContent = message;
        refs.toast.classList.toggle('toast--visible', true);
        refs.toast.style.background = isError ? '#2f2f2f' : '#1f1f1f';
        setTimeout(() => {
            refs.toast.classList.remove('toast--visible');
        }, 3200);
    }

    async function checkServerStatus() {
        try {
            const response = await fetch('/');
            if (response.ok) {
                refs.statusIndicator.style.background = '#d0d0d0';
                refs.statusText.textContent = '正常運行';
            } else {
                throw new Error('狀態異常');
            }
        } catch (error) {
            refs.statusIndicator.style.background = '#616161';
            refs.statusText.textContent = '連線異常';
        }
    }

    function updateAdvancedCollectionOptions() {
        const options = state.collections
            .map(collection => `<option value="${collection.name}">${collection.name}</option>`)
            .join('');
        const placeholder = '<option value="">選擇 Collection</option>';
        refs.advancedCollection.innerHTML = placeholder + options;
        if (state.currentCollection) {
            refs.advancedCollection.value = state.currentCollection;
        }
    }

    function toggleModal(modal, visible) {
        if (!modal) return;
        const isOpening = visible && modal.getAttribute('aria-hidden') !== 'false';
        modal.setAttribute('aria-hidden', visible ? 'false' : 'true');
        if (isOpening && modal === refs.modalCreateCollection) {
            resetCreateCollectionForm();
            document.getElementById('new-collection-name').value = '';
            document.getElementById('new-primary-key').value = 'id';
            document.getElementById('fts-stemming').checked = true;
        }
    }
})();
