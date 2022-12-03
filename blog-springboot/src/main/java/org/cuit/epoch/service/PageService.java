package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.entity.Page;
import org.cuit.epoch.vo.PageVO;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:49
 * @description:
 */
public interface PageService extends IService<Page> {

    /**
     * 保存或更新页面
     *
     * @param pageVO 页面信息
     */
    void saveOrUpdatePage(PageVO pageVO);

    /**
     * 删除页面
     *
     * @param pageId 页面id
     */
    void deletePage(Integer pageId);

    /**
     * 获取页面列表
     *
     * @return {@link List <PageVO>} 页面列表
     */
    List<PageVO> listPages();

}