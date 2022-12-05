package org.cuit.epoch.controller;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.log.OperationLogDTO;
import org.cuit.epoch.entity.OperationLog;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.page.PageResult;

public interface OperationLogService extends IService<OperationLog> {

    /**
     * 查询日志列表
     *
     * @param conditionVO 条件
     * @return 日志列表
     */
    PageResult<OperationLogDTO> listOperationLogs(ConditionVO conditionVO);

}
