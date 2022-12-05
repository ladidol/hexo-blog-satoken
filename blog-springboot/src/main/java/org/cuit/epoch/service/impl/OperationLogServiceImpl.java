package org.cuit.epoch.service.impl;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.cuit.epoch.controller.OperationLogService;
import org.cuit.epoch.dto.log.OperationLogDTO;
import org.cuit.epoch.entity.OperationLog;
import org.cuit.epoch.mapper.OperationLogMapper;
import org.cuit.epoch.util.BeanCopyUtils;
import org.cuit.epoch.util.PageUtils;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.stereotype.Service;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/12/5 17:25
 * @description:
 */
@Service
public class OperationLogServiceImpl extends ServiceImpl<OperationLogMapper, OperationLog> implements OperationLogService {

    @Override
    public PageResult<OperationLogDTO> listOperationLogs(ConditionVO conditionVO) {
        Page<OperationLog> page = new Page<>(PageUtils.getCurrent(), PageUtils.getSize());
        // 查询日志列表
        Page<OperationLog> operationLogPage = this.page(page, new LambdaQueryWrapper<OperationLog>()
                .like(StringUtils.isNotBlank(conditionVO.getKeywords()), OperationLog::getOptModule, conditionVO.getKeywords())
                .or()
                .like(StringUtils.isNotBlank(conditionVO.getKeywords()), OperationLog::getOptDesc, conditionVO.getKeywords())
                .orderByDesc(OperationLog::getId));
        List<OperationLogDTO> operationLogDTOList = BeanCopyUtils.copyList(operationLogPage.getRecords(), OperationLogDTO.class);
        return new PageResult<>(operationLogDTOList, (int) operationLogPage.getTotal());
    }

}