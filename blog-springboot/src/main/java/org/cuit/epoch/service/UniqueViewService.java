package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.blog.UniqueViewDTO;
import org.cuit.epoch.entity.UniqueView;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/26 0:01
 * @description: {}
 */
public interface UniqueViewService extends IService<UniqueView> {

    /**
     * 获取7天用户量统计
     *
     * @return 用户量
     */
    List<UniqueViewDTO> listUniqueViews();

}
