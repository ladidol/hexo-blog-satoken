package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.LabelOptionDTO;
import org.cuit.epoch.dto.menu.MenuDTO;
import org.cuit.epoch.dto.menu.UserMenuDTO;
import org.cuit.epoch.entity.Menu;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.MenuVO;

import java.util.List;
/**
 * @author: ladidol
 * @date: 2022/11/25 12:38
 * @description: 后台菜单服务
 */
public interface MenuService extends IService<Menu> {

    /**
     * 查看菜单列表
     *
     * @param conditionVO 条件
     * @return 菜单列表
     */
    List<MenuDTO> listMenus(ConditionVO conditionVO);

    /**
     * 新增或修改菜单
     *
     * @param menuVO 菜单信息
     */
    void saveOrUpdateMenu(MenuVO menuVO);

    /**
     * 删除菜单
     *
     * @param menuId 菜单id
     */
    void deleteMenu(Integer menuId);

    /**
     * 查看角色菜单选项
     *
     * @return 角色菜单选项
     */
    List<LabelOptionDTO> listMenuOptions();

    /**
     * 查看用户菜单
     *
     * @return 菜单列表
     */
    List<UserMenuDTO> listUserMenus();

}